package iothub

import (
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/provisioningservices/mgmt/2018-01-22/iothub"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/iothub/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceIotHubDPSCertificate() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceIotHubDPSCertificateCreateUpdate,
		Read:   resourceIotHubDPSCertificateRead,
		Update: resourceIotHubDPSCertificateCreateUpdate,
		Delete: resourceIotHubDPSCertificateDelete,

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
				ValidateFunc: validate.IoTHubName,
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"iot_dps_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.IoTHubName,
			},

			"certificate_content": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Sensitive:    true,
			},
		},
	}
}

func resourceIotHubDPSCertificateCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.DPSCertificateClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)
	iotDPSName := d.Get("iot_dps_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, name, resourceGroup, iotDPSName, "")
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_iothub_dps_certificate", *existing.ID)
		}
	}

	certificate := iothub.CertificateBodyDescription{
		Certificate: utils.String(d.Get("certificate_content").(string)),
	}

	if _, err := client.CreateOrUpdate(ctx, resourceGroup, iotDPSName, name, certificate, ""); err != nil {
		return fmt.Errorf("Error creating/updating IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}

	resp, err := client.Get(ctx, name, resourceGroup, iotDPSName, "")
	if err != nil {
		return fmt.Errorf("Error retrieving IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}

	if resp.ID == nil {
		return fmt.Errorf("Cannot read IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}

	d.SetId(*resp.ID)

	return resourceIotHubDPSCertificateRead(d, meta)
}

func resourceIotHubDPSCertificateRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.DPSCertificateClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	iotDPSName := id.Path["provisioningServices"]
	name := id.Path["certificates"]

	resp, err := client.Get(ctx, name, resourceGroup, iotDPSName, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error retrieving IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}

	d.Set("name", resp.Name)
	d.Set("resource_group_name", resourceGroup)
	d.Set("iot_dps_name", iotDPSName)
	// We are unable to set `certificate_content` since it is not returned from the API

	return nil
}

func resourceIotHubDPSCertificateDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.DPSCertificateClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	iotDPSName := id.Path["provisioningServices"]
	name := id.Path["certificates"]

	resp, err := client.Get(ctx, name, resourceGroup, iotDPSName, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			return nil
		}
		return fmt.Errorf("Error retrieving IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}

	if resp.Etag == nil {
		return fmt.Errorf("Error deleting IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q) because Etag is nil", name, iotDPSName, resourceGroup)
	}

	// TODO address this delete call if https://github.com/Azure/azure-rest-api-specs/pull/6311 get's merged
	if _, err := client.Delete(ctx, resourceGroup, *resp.Etag, iotDPSName, name, "", nil, nil, iothub.ServerAuthentication, nil, nil, nil, ""); err != nil {
		return fmt.Errorf("Error deleting IoT Device Provisioning Service Certificate %q (Device Provisioning Service %q / Resource Group %q): %+v", name, iotDPSName, resourceGroup, err)
	}
	return nil
}
