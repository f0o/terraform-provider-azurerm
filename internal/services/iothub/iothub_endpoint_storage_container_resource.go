package iothub

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/iothub/mgmt/2020-03-01/devices"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/locks"
	iothubValidate "github.com/hashicorp/terraform-provider-azurerm/internal/services/iothub/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/storage/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceIotHubEndpointStorageContainer() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceIotHubEndpointStorageContainerCreateUpdate,
		Read:   resourceIotHubEndpointStorageContainerRead,
		Update: resourceIotHubEndpointStorageContainerCreateUpdate,
		Delete: resourceIotHubEndpointStorageContainerDelete,
		// TODO: replace this with an importer which validates the ID during import
		Importer: pluginsdk.DefaultImporter(),

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*pluginsdk.Schema{
			"name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: iothubValidate.IoTHubEndpointName,
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"iothub_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: iothubValidate.IoTHubName,
			},

			"container_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validate.StorageContainerName,
			},

			"file_name_format": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Default:  false,
			},

			"batch_frequency_in_seconds": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				Default:      300,
				ValidateFunc: validation.IntBetween(60, 720),
			},

			"max_chunk_size_in_bytes": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				Default:      314572800,
				ValidateFunc: validation.IntBetween(10485760, 524288000),
			},

			"connection_string": {
				Type:     pluginsdk.TypeString,
				Required: true,
				DiffSuppressFunc: func(k, old, new string, d *pluginsdk.ResourceData) bool {
					accountKeyRegex := regexp.MustCompile("AccountKey=[^;]+")

					maskedNew := accountKeyRegex.ReplaceAllString(new, "AccountKey=****")
					return (new == d.Get(k).(string)) && (maskedNew == old)
				},
				Sensitive: true,
			},

			"encoding": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				ValidateFunc: validation.StringInSlice([]string{
					string(devices.Avro),
					string(devices.AvroDeflate),
					string(devices.JSON),
				}, true),
			},
		},
	}
}

func resourceIotHubEndpointStorageContainerCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()
	subscriptionID := meta.(*clients.Client).Account.SubscriptionId

	iothubName := d.Get("iothub_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	locks.ByName(iothubName, IothubResourceName)
	defer locks.UnlockByName(iothubName, IothubResourceName)

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		if utils.ResponseWasNotFound(iothub.Response) {
			return fmt.Errorf("IotHub %q (Resource Group %q) was not found", iothubName, resourceGroup)
		}

		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	endpointName := d.Get("name").(string)
	resourceId := fmt.Sprintf("%s/Endpoints/%s", *iothub.ID, endpointName)

	connectionStr := d.Get("connection_string").(string)
	containerName := d.Get("container_name").(string)
	fileNameFormat := d.Get("file_name_format").(string)
	batchFrequencyInSeconds := int32(d.Get("batch_frequency_in_seconds").(int))
	maxChunkSizeInBytes := int32(d.Get("max_chunk_size_in_bytes").(int))
	encoding := d.Get("encoding").(string)

	storageContainerEndpoint := devices.RoutingStorageContainerProperties{
		ConnectionString:        &connectionStr,
		Name:                    &endpointName,
		SubscriptionID:          &subscriptionID,
		ResourceGroup:           &resourceGroup,
		ContainerName:           &containerName,
		FileNameFormat:          &fileNameFormat,
		BatchFrequencyInSeconds: &batchFrequencyInSeconds,
		MaxChunkSizeInBytes:     &maxChunkSizeInBytes,
		Encoding:                devices.Encoding(encoding),
	}

	routing := iothub.Properties.Routing

	if routing == nil {
		routing = &devices.RoutingProperties{}
	}

	if routing.Endpoints == nil {
		routing.Endpoints = &devices.RoutingEndpoints{}
	}

	if routing.Endpoints.StorageContainers == nil {
		storageContainers := make([]devices.RoutingStorageContainerProperties, 0)
		routing.Endpoints.StorageContainers = &storageContainers
	}

	endpoints := make([]devices.RoutingStorageContainerProperties, 0)

	alreadyExists := false
	for _, existingEndpoint := range *routing.Endpoints.StorageContainers {
		if existingEndpointName := existingEndpoint.Name; existingEndpointName != nil {
			if strings.EqualFold(*existingEndpointName, endpointName) {
				if d.IsNewResource() {
					return tf.ImportAsExistsError("azurerm_iothub_endpoint_storage_container", resourceId)
				}
				endpoints = append(endpoints, storageContainerEndpoint)
				alreadyExists = true
			} else {
				endpoints = append(endpoints, existingEndpoint)
			}
		}
	}

	if d.IsNewResource() {
		endpoints = append(endpoints, storageContainerEndpoint)
	} else if !alreadyExists {
		return fmt.Errorf("Unable to find Storage Container Endpoint %q defined for IotHub %q (Resource Group %q)", endpointName, iothubName, resourceGroup)
	}
	routing.Endpoints.StorageContainers = &endpoints

	future, err := client.CreateOrUpdate(ctx, resourceGroup, iothubName, iothub, "")
	if err != nil {
		return fmt.Errorf("Error creating/updating IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for the completion of the creating/updating of IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	d.SetId(resourceId)

	return resourceIotHubEndpointStorageContainerRead(d, meta)
}

func resourceIotHubEndpointStorageContainerRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	parsedIothubEndpointId, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := parsedIothubEndpointId.ResourceGroup
	iothubName := parsedIothubEndpointId.Path["IotHubs"]
	endpointName := parsedIothubEndpointId.Path["Endpoints"]

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	d.Set("name", endpointName)
	d.Set("iothub_name", iothubName)
	d.Set("resource_group_name", resourceGroup)

	if iothub.Properties == nil || iothub.Properties.Routing == nil || iothub.Properties.Routing.Endpoints == nil {
		return nil
	}

	if endpoints := iothub.Properties.Routing.Endpoints.StorageContainers; endpoints != nil {
		for _, endpoint := range *endpoints {
			if existingEndpointName := endpoint.Name; existingEndpointName != nil {
				if strings.EqualFold(*existingEndpointName, endpointName) {
					d.Set("connection_string", endpoint.ConnectionString)
					d.Set("container_name", endpoint.ContainerName)
					d.Set("file_name_format", endpoint.FileNameFormat)
					d.Set("batch_frequency_in_seconds", endpoint.BatchFrequencyInSeconds)
					d.Set("max_chunk_size_in_bytes", endpoint.MaxChunkSizeInBytes)
					d.Set("encoding", endpoint.Encoding)
				}
			}
		}
	}

	return nil
}

func resourceIotHubEndpointStorageContainerDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	parsedIothubEndpointId, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := parsedIothubEndpointId.ResourceGroup
	iothubName := parsedIothubEndpointId.Path["IotHubs"]
	endpointName := parsedIothubEndpointId.Path["Endpoints"]

	locks.ByName(iothubName, IothubResourceName)
	defer locks.UnlockByName(iothubName, IothubResourceName)

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		if utils.ResponseWasNotFound(iothub.Response) {
			return fmt.Errorf("IotHub %q (Resource Group %q) was not found", iothubName, resourceGroup)
		}

		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	if iothub.Properties == nil || iothub.Properties.Routing == nil || iothub.Properties.Routing.Endpoints == nil {
		return nil
	}
	endpoints := iothub.Properties.Routing.Endpoints.StorageContainers

	if endpoints == nil {
		return nil
	}

	updatedEndpoints := make([]devices.RoutingStorageContainerProperties, 0)
	for _, endpoint := range *endpoints {
		if existingEndpointName := endpoint.Name; existingEndpointName != nil {
			if !strings.EqualFold(*existingEndpointName, endpointName) {
				updatedEndpoints = append(updatedEndpoints, endpoint)
			}
		}
	}
	iothub.Properties.Routing.Endpoints.StorageContainers = &updatedEndpoints

	future, err := client.CreateOrUpdate(ctx, resourceGroup, iothubName, iothub, "")
	if err != nil {
		return fmt.Errorf("Error updating IotHub %q (Resource Group %q) with Storage Container Endpoint %q: %+v", iothubName, resourceGroup, endpointName, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for IotHub %q (Resource Group %q) to finish updating Storage Container Endpoint %q: %+v", iothubName, resourceGroup, endpointName, err)
	}

	return nil
}
