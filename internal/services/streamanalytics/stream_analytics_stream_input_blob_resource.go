package streamanalytics

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/preview/streamanalytics/mgmt/2020-03-01-preview/streamanalytics"
	"github.com/hashicorp/go-azure-helpers/response"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/streamanalytics/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceStreamAnalyticsStreamInputBlob() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceStreamAnalyticsStreamInputBlobCreateUpdate,
		Read:   resourceStreamAnalyticsStreamInputBlobRead,
		Update: resourceStreamAnalyticsStreamInputBlobCreateUpdate,
		Delete: resourceStreamAnalyticsStreamInputBlobDelete,
		Importer: pluginsdk.ImporterValidatingResourceId(func(id string) error {
			_, err := parse.StreamInputID(id)
			return err
		}),

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
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"stream_analytics_job_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"date_format": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"path_pattern": {
				Type:     pluginsdk.TypeString,
				Required: true,
			},

			"storage_account_key": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				Sensitive:    true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"storage_account_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"storage_container_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"time_format": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"serialization": schemaStreamAnalyticsStreamInputSerialization(),
		},
	}
}

func resourceStreamAnalyticsStreamInputBlobCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.InputsClient
	subscriptionId := meta.(*clients.Client).Account.SubscriptionId
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for Azure Stream Analytics Stream Input Blob creation.")
	resourceId := parse.NewStreamInputID(subscriptionId, d.Get("resource_group_name").(string), d.Get("stream_analytics_job_name").(string), d.Get("name").(string))
	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceId.ResourceGroup, resourceId.StreamingjobName, resourceId.InputName)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("checking for presence of existing %s: %+v", resourceId, err)
			}
		}

		if !utils.ResponseWasNotFound(existing.Response) {
			return tf.ImportAsExistsError("azurerm_stream_analytics_stream_input_blob", resourceId.ID())
		}
	}

	containerName := d.Get("storage_container_name").(string)
	dateFormat := d.Get("date_format").(string)
	pathPattern := d.Get("path_pattern").(string)
	storageAccountKey := d.Get("storage_account_key").(string)
	storageAccountName := d.Get("storage_account_name").(string)
	timeFormat := d.Get("time_format").(string)

	serializationRaw := d.Get("serialization").([]interface{})
	serialization, err := expandStreamAnalyticsStreamInputSerialization(serializationRaw)
	if err != nil {
		return fmt.Errorf("Error expanding `serialization`: %+v", err)
	}

	props := streamanalytics.Input{
		Name: utils.String(resourceId.InputName),
		Properties: &streamanalytics.StreamInputProperties{
			Type: streamanalytics.TypeStream,
			Datasource: &streamanalytics.BlobStreamInputDataSource{
				Type: streamanalytics.TypeBasicStreamInputDataSourceTypeMicrosoftStorageBlob,
				BlobStreamInputDataSourceProperties: &streamanalytics.BlobStreamInputDataSourceProperties{
					Container:   utils.String(containerName),
					DateFormat:  utils.String(dateFormat),
					PathPattern: utils.String(pathPattern),
					TimeFormat:  utils.String(timeFormat),
					StorageAccounts: &[]streamanalytics.StorageAccount{
						{
							AccountName: utils.String(storageAccountName),
							AccountKey:  utils.String(storageAccountKey),
						},
					},
				},
			},
			Serialization: serialization,
		},
	}

	if d.IsNewResource() {
		if _, err := client.CreateOrReplace(ctx, props, resourceId.ResourceGroup, resourceId.StreamingjobName, resourceId.InputName, "", ""); err != nil {
			return fmt.Errorf("creating %s: %+v", resourceId, err)
		}

		d.SetId(resourceId.ID())
	} else if _, err := client.Update(ctx, props, resourceId.ResourceGroup, resourceId.StreamingjobName, resourceId.InputName, ""); err != nil {
		return fmt.Errorf("updating %s: %+v", resourceId, err)
	}

	return resourceStreamAnalyticsStreamInputBlobRead(d, meta)
}

func resourceStreamAnalyticsStreamInputBlobRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.InputsClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.StreamInputID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.StreamingjobName, id.InputName)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] %s - removing from state!", id)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("retrieving %s: %+v", id, err)
	}

	d.Set("name", id.InputName)
	d.Set("stream_analytics_job_name", id.StreamingjobName)
	d.Set("resource_group_name", id.ResourceGroup)

	if props := resp.Properties; props != nil {
		v, ok := props.AsStreamInputProperties()
		if !ok {
			return fmt.Errorf("converting Stream Input Blob to an Stream Input: %+v", err)
		}

		eventHub, ok := v.Datasource.AsBlobStreamInputDataSource()
		if !ok {
			return fmt.Errorf("converting Stream Input Blob to an Blob Stream Input: %+v", err)
		}

		d.Set("date_format", eventHub.DateFormat)
		d.Set("path_pattern", eventHub.PathPattern)
		d.Set("storage_container_name", eventHub.Container)
		d.Set("time_format", eventHub.TimeFormat)

		if accounts := eventHub.StorageAccounts; accounts != nil && len(*accounts) > 0 {
			account := (*accounts)[0]
			d.Set("storage_account_name", account.AccountName)
		}

		if err := d.Set("serialization", flattenStreamAnalyticsStreamInputSerialization(v.Serialization)); err != nil {
			return fmt.Errorf("setting `serialization`: %+v", err)
		}
	}

	return nil
}

func resourceStreamAnalyticsStreamInputBlobDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.InputsClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.StreamInputID(d.Id())
	if err != nil {
		return err
	}

	if resp, err := client.Delete(ctx, id.ResourceGroup, id.StreamingjobName, id.InputName); err != nil {
		if !response.WasNotFound(resp.Response) {
			return fmt.Errorf("deleting %s: %+v", id, err)
		}
	}

	return nil
}
