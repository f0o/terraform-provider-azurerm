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

func resourceStreamAnalyticsOutputServiceBusTopic() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceStreamAnalyticsOutputServiceBusTopicCreateUpdate,
		Read:   resourceStreamAnalyticsOutputServiceBusTopicRead,
		Update: resourceStreamAnalyticsOutputServiceBusTopicCreateUpdate,
		Delete: resourceStreamAnalyticsOutputServiceBusTopicDelete,
		Importer: pluginsdk.ImporterValidatingResourceId(func(id string) error {
			_, err := parse.OutputID(id)
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

			"topic_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"servicebus_namespace": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"shared_access_policy_key": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				Sensitive:    true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"shared_access_policy_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"serialization": schemaStreamAnalyticsOutputSerialization(),
		},
	}
}

func resourceStreamAnalyticsOutputServiceBusTopicCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.OutputsClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for Azure Stream Analytics Output ServiceBus Topic creation.")
	name := d.Get("name").(string)
	jobName := d.Get("stream_analytics_job_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceGroup, jobName, name)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Stream Analytics Output ServiceBus Topic %q (Job %q / Resource Group %q): %s", name, jobName, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_stream_analytics_output_servicebus_topic", *existing.ID)
		}
	}

	topicName := d.Get("topic_name").(string)
	serviceBusNamespace := d.Get("servicebus_namespace").(string)
	sharedAccessPolicyKey := d.Get("shared_access_policy_key").(string)
	sharedAccessPolicyName := d.Get("shared_access_policy_name").(string)

	serializationRaw := d.Get("serialization").([]interface{})
	serialization, err := expandStreamAnalyticsOutputSerialization(serializationRaw)
	if err != nil {
		return fmt.Errorf("Error expanding `serialization`: %+v", err)
	}

	props := streamanalytics.Output{
		Name: utils.String(name),
		OutputProperties: &streamanalytics.OutputProperties{
			Datasource: &streamanalytics.ServiceBusTopicOutputDataSource{
				Type: streamanalytics.TypeMicrosoftServiceBusTopic,
				ServiceBusTopicOutputDataSourceProperties: &streamanalytics.ServiceBusTopicOutputDataSourceProperties{
					TopicName:              utils.String(topicName),
					ServiceBusNamespace:    utils.String(serviceBusNamespace),
					SharedAccessPolicyKey:  utils.String(sharedAccessPolicyKey),
					SharedAccessPolicyName: utils.String(sharedAccessPolicyName),
				},
			},
			Serialization: serialization,
		},
	}

	if d.IsNewResource() {
		if _, err := client.CreateOrReplace(ctx, props, resourceGroup, jobName, name, "", ""); err != nil {
			return fmt.Errorf("Error Creating Stream Analytics Output ServiceBus Topic %q (Job %q / Resource Group %q): %+v", name, jobName, resourceGroup, err)
		}

		read, err := client.Get(ctx, resourceGroup, jobName, name)
		if err != nil {
			return fmt.Errorf("Error retrieving Stream Analytics Output ServiceBus Topic %q (Job %q / Resource Group %q): %+v", name, jobName, resourceGroup, err)
		}
		if read.ID == nil {
			return fmt.Errorf("Cannot read ID of Stream Analytics Output ServiceBus Topic %q (Job %q / Resource Group %q)", name, jobName, resourceGroup)
		}

		d.SetId(*read.ID)
	} else if _, err := client.Update(ctx, props, resourceGroup, jobName, name, ""); err != nil {
		return fmt.Errorf("Error Updating Stream Analytics Output ServiceBus Topic %q (Job %q / Resource Group %q): %+v", name, jobName, resourceGroup, err)
	}

	return resourceStreamAnalyticsOutputServiceBusTopicRead(d, meta)
}

func resourceStreamAnalyticsOutputServiceBusTopicRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.OutputsClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.OutputID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.StreamingjobName, id.Name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] %s was not found - removing from state!", id)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("retrieving %s: %+v", id, err)
	}

	d.Set("name", id.Name)
	d.Set("stream_analytics_job_name", id.StreamingjobName)
	d.Set("resource_group_name", id.ResourceGroup)

	if props := resp.OutputProperties; props != nil {
		v, ok := props.Datasource.AsServiceBusTopicOutputDataSource()
		if !ok {
			return fmt.Errorf("converting Output Data Source to a ServiceBus Topic Output: %+v", err)
		}

		d.Set("topic_name", v.TopicName)
		d.Set("servicebus_namespace", v.ServiceBusNamespace)
		d.Set("shared_access_policy_name", v.SharedAccessPolicyName)

		if err := d.Set("serialization", flattenStreamAnalyticsOutputSerialization(props.Serialization)); err != nil {
			return fmt.Errorf("setting `serialization`: %+v", err)
		}
	}

	return nil
}

func resourceStreamAnalyticsOutputServiceBusTopicDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).StreamAnalytics.OutputsClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.OutputID(d.Id())
	if err != nil {
		return err
	}

	if resp, err := client.Delete(ctx, id.ResourceGroup, id.StreamingjobName, id.Name); err != nil {
		if !response.WasNotFound(resp.Response) {
			return fmt.Errorf("deleting %s: %+v", id, err)
		}
	}

	return nil
}
