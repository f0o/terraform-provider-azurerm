package datafactory

import (
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/datafactory/mgmt/2018-06-01/datafactory"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/datafactory/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/datafactory/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceDataFactoryLinkedServiceSQLServer() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceDataFactoryLinkedServiceSQLServerCreateUpdate,
		Read:   resourceDataFactoryLinkedServiceSQLServerRead,
		Update: resourceDataFactoryLinkedServiceSQLServerCreateUpdate,
		Delete: resourceDataFactoryLinkedServiceSQLServerDelete,

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
				ValidateFunc: validate.LinkedServiceDatasetName,
			},

			"data_factory_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.DataFactoryName(),
			},

			// There's a bug in the Azure API where this is returned in lower-case
			// BUG: https://github.com/Azure/azure-rest-api-specs/issues/5788
			"resource_group_name": azure.SchemaResourceGroupNameDiffSuppress(),

			"connection_string": {
				Type:             pluginsdk.TypeString,
				Optional:         true,
				ExactlyOneOf:     []string{"connection_string", "key_vault_connection_string"},
				DiffSuppressFunc: azureRmDataFactoryLinkedServiceConnectionStringDiff,
				ValidateFunc:     validation.StringIsNotEmpty,
			},

			"key_vault_connection_string": {
				Type:         pluginsdk.TypeList,
				Optional:     true,
				ExactlyOneOf: []string{"connection_string", "key_vault_connection_string"},
				MaxItems:     1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"linked_service_name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},

						"secret_name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
					},
				},
			},

			"key_vault_password": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"linked_service_name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},

						"secret_name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
					},
				},
			},

			"description": {
				Type:         pluginsdk.TypeString,
				Optional:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"integration_runtime_name": {
				Type:         pluginsdk.TypeString,
				Optional:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"parameters": {
				Type:     pluginsdk.TypeMap,
				Optional: true,
				Elem: &pluginsdk.Schema{
					Type:         pluginsdk.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
			},

			"annotations": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				Elem: &pluginsdk.Schema{
					Type:         pluginsdk.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
			},

			"additional_properties": {
				Type:     pluginsdk.TypeMap,
				Optional: true,
				Elem: &pluginsdk.Schema{
					Type:         pluginsdk.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
			},
		},
	}
}

func resourceDataFactoryLinkedServiceSQLServerCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.LinkedServiceClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	dataFactoryName := d.Get("data_factory_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceGroup, dataFactoryName, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", name, dataFactoryName, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_data_factory_linked_service_sql_server", *existing.ID)
		}
	}

	password := d.Get("key_vault_password").([]interface{})

	sqlServerLinkedService := &datafactory.SQLServerLinkedService{
		Description: utils.String(d.Get("description").(string)),
		SQLServerLinkedServiceTypeProperties: &datafactory.SQLServerLinkedServiceTypeProperties{
			Password: expandAzureKeyVaultSecretReference(password),
		},
		Type: datafactory.TypeBasicLinkedServiceTypeSQLServer,
	}

	if v, ok := d.GetOk("connection_string"); ok {
		sqlServerLinkedService.SQLServerLinkedServiceTypeProperties.ConnectionString = v.(string)
	}

	if v, ok := d.GetOk("key_vault_connection_string"); ok {
		sqlServerLinkedService.SQLServerLinkedServiceTypeProperties.ConnectionString = expandAzureKeyVaultSecretReference(v.([]interface{}))
	}

	if v, ok := d.GetOk("parameters"); ok {
		sqlServerLinkedService.Parameters = expandDataFactoryParameters(v.(map[string]interface{}))
	}

	if v, ok := d.GetOk("integration_runtime_name"); ok {
		sqlServerLinkedService.ConnectVia = expandDataFactoryLinkedServiceIntegrationRuntime(v.(string))
	}

	if v, ok := d.GetOk("additional_properties"); ok {
		sqlServerLinkedService.AdditionalProperties = v.(map[string]interface{})
	}

	if v, ok := d.GetOk("annotations"); ok {
		annotations := v.([]interface{})
		sqlServerLinkedService.Annotations = &annotations
	}

	linkedService := datafactory.LinkedServiceResource{
		Properties: sqlServerLinkedService,
	}

	if _, err := client.CreateOrUpdate(ctx, resourceGroup, dataFactoryName, name, linkedService, ""); err != nil {
		return fmt.Errorf("Error creating/updating Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", name, dataFactoryName, resourceGroup, err)
	}

	resp, err := client.Get(ctx, resourceGroup, dataFactoryName, name, "")
	if err != nil {
		return fmt.Errorf("Error retrieving Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", name, dataFactoryName, resourceGroup, err)
	}

	if resp.ID == nil {
		return fmt.Errorf("Cannot read Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", name, dataFactoryName, resourceGroup, err)
	}

	d.SetId(*resp.ID)

	return resourceDataFactoryLinkedServiceSQLServerRead(d, meta)
}

func resourceDataFactoryLinkedServiceSQLServerRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.LinkedServiceClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.LinkedServiceID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.FactoryName, id.Name, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error retrieving Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", id.Name, id.FactoryName, id.ResourceGroup, err)
	}

	d.Set("name", id.Name)
	d.Set("resource_group_name", id.ResourceGroup)
	d.Set("data_factory_name", id.FactoryName)

	sqlServer, ok := resp.Properties.AsSQLServerLinkedService()
	if !ok {
		return fmt.Errorf("Error classifying Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): Expected: %q Received: %q", id.Name, id.FactoryName, id.ResourceGroup, datafactory.TypeBasicLinkedServiceTypeSQLServer, *resp.Type)
	}

	d.Set("additional_properties", sqlServer.AdditionalProperties)
	d.Set("description", sqlServer.Description)

	annotations := flattenDataFactoryAnnotations(sqlServer.Annotations)
	if err := d.Set("annotations", annotations); err != nil {
		return fmt.Errorf("Error setting `annotations`: %+v", err)
	}

	parameters := flattenDataFactoryParameters(sqlServer.Parameters)
	if err := d.Set("parameters", parameters); err != nil {
		return fmt.Errorf("Error setting `parameters`: %+v", err)
	}

	if connectVia := sqlServer.ConnectVia; connectVia != nil {
		if connectVia.ReferenceName != nil {
			d.Set("integration_runtime_name", connectVia.ReferenceName)
		}
	}

	if properties := sqlServer.SQLServerLinkedServiceTypeProperties; properties != nil {
		if properties.ConnectionString != nil {
			if val, ok := properties.ConnectionString.(map[string]interface{}); ok {
				if err := d.Set("key_vault_connection_string", flattenAzureKeyVaultConnectionString(val)); err != nil {
					return fmt.Errorf("setting `key_vault_connection_string`: %+v", err)
				}
			} else if val, ok := properties.ConnectionString.(string); ok {
				d.Set("connection_string", val)
			} else {
				return fmt.Errorf("setting `connection_string`: %+v", err)
			}
		}

		if password := properties.Password; password != nil {
			if keyVaultPassword, ok := password.AsAzureKeyVaultSecretReference(); ok {
				if err := d.Set("key_vault_password", flattenAzureKeyVaultSecretReference(keyVaultPassword)); err != nil {
					return fmt.Errorf("setting `key_vault_password`: %+v", err)
				}
			}
		}
	}

	return nil
}

func resourceDataFactoryLinkedServiceSQLServerDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.LinkedServiceClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.LinkedServiceID(d.Id())
	if err != nil {
		return err
	}

	response, err := client.Delete(ctx, id.ResourceGroup, id.FactoryName, id.Name)
	if err != nil {
		if !utils.ResponseWasNotFound(response) {
			return fmt.Errorf("Error deleting Data Factory Linked Service SQL Server %q (Data Factory %q / Resource Group %q): %+v", id.Name, id.FactoryName, id.ResourceGroup, err)
		}
	}

	return nil
}
