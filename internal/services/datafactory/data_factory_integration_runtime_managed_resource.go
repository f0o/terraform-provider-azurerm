package datafactory

import (
	"fmt"
	"regexp"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/datafactory/mgmt/2018-06-01/datafactory"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/features"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/datafactory/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceDataFactoryIntegrationRuntimeManaged() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceDataFactoryIntegrationRuntimeManagedCreateUpdate,
		Read:   resourceDataFactoryIntegrationRuntimeManagedRead,
		Update: resourceDataFactoryIntegrationRuntimeManagedCreateUpdate,
		Delete: resourceDataFactoryIntegrationRuntimeManagedDelete,
		// TODO: replace this with an importer which validates the ID during import
		Importer: pluginsdk.DefaultImporter(),

		DeprecationMessage: features.DeprecatedInThreePointOh("The resource 'azurerm_data_factory_integration_runtime_managed' has been superseded by the 'azurerm_data_factory_integration_runtime_azure_ssis'."),

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*pluginsdk.Schema{
			"name": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.StringMatch(
					regexp.MustCompile(`^([a-zA-Z0-9](-|-?[a-zA-Z0-9]+)+[a-zA-Z0-9])$`),
					`Invalid name for Managed Integration Runtime: minimum 3 characters, must start and end with a number or a letter, may only consist of letters, numbers and dashes and no consecutive dashes.`,
				),
			},

			"description": {
				Type:     pluginsdk.TypeString,
				Optional: true,
			},

			"data_factory_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.DataFactoryName(),
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"location": azure.SchemaLocation(),

			"node_size": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringInSlice([]string{
					"Standard_D2_v3",
					"Standard_D4_v3",
					"Standard_D8_v3",
					"Standard_D16_v3",
					"Standard_D32_v3",
					"Standard_D64_v3",
					"Standard_E2_v3",
					"Standard_E4_v3",
					"Standard_E8_v3",
					"Standard_E16_v3",
					"Standard_E32_v3",
					"Standard_E64_v3",
					"Standard_D1_v2",
					"Standard_D2_v2",
					"Standard_D3_v2",
					"Standard_D4_v2",
					"Standard_A4_v2",
					"Standard_A8_v2",
				}, false),
			},

			"number_of_nodes": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				Default:      1,
				ValidateFunc: validation.IntBetween(1, 10),
			},

			"max_parallel_executions_per_node": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				Default:      1,
				ValidateFunc: validation.IntBetween(1, 16),
			},

			"edition": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Default:  string(datafactory.IntegrationRuntimeEditionStandard),
				ValidateFunc: validation.StringInSlice([]string{
					string(datafactory.IntegrationRuntimeEditionStandard),
					string(datafactory.IntegrationRuntimeEditionEnterprise),
				}, false),
			},

			"license_type": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Default:  string(datafactory.IntegrationRuntimeLicenseTypeLicenseIncluded),
				ValidateFunc: validation.StringInSlice([]string{
					string(datafactory.IntegrationRuntimeLicenseTypeLicenseIncluded),
					string(datafactory.IntegrationRuntimeLicenseTypeBasePrice),
				}, false),
			},

			"vnet_integration": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"vnet_id": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: azure.ValidateResourceID,
						},
						"subnet_name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
					},
				},
			},

			"custom_setup_script": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"blob_container_uri": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"sas_token": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							Sensitive:    true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
					},
				},
			},

			"catalog_info": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"server_endpoint": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"administrator_login": {
							Type:         pluginsdk.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"administrator_password": {
							Type:         pluginsdk.TypeString,
							Optional:     true,
							Sensitive:    true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"pricing_tier": {
							Type:     pluginsdk.TypeString,
							Optional: true,
							Default:  string(datafactory.IntegrationRuntimeSsisCatalogPricingTierBasic),
							ValidateFunc: validation.StringInSlice([]string{
								string(datafactory.IntegrationRuntimeSsisCatalogPricingTierBasic),
								string(datafactory.IntegrationRuntimeSsisCatalogPricingTierStandard),
								string(datafactory.IntegrationRuntimeSsisCatalogPricingTierPremium),
								string(datafactory.IntegrationRuntimeSsisCatalogPricingTierPremiumRS),
							}, false),
						},
					},
				},
			},
		},
	}
}

func resourceDataFactoryIntegrationRuntimeManagedCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.IntegrationRuntimesClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	factoryName := d.Get("data_factory_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceGroup, factoryName, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q): %s", name, resourceGroup, factoryName, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_data_factory_integration_runtime_managed", *existing.ID)
		}
	}

	description := d.Get("description").(string)
	managedIntegrationRuntime := datafactory.ManagedIntegrationRuntime{
		Description: &description,
		Type:        datafactory.TypeBasicIntegrationRuntimeTypeManaged,
		ManagedIntegrationRuntimeTypeProperties: &datafactory.ManagedIntegrationRuntimeTypeProperties{
			ComputeProperties: expandDataFactoryIntegrationRuntimeManagedComputeProperties(d),
			SsisProperties:    expandDataFactoryIntegrationRuntimeManagedSsisProperties(d),
		},
	}

	basicIntegrationRuntime, _ := managedIntegrationRuntime.AsBasicIntegrationRuntime()

	integrationRuntime := datafactory.IntegrationRuntimeResource{
		Name:       &name,
		Properties: basicIntegrationRuntime,
	}

	if _, err := client.CreateOrUpdate(ctx, resourceGroup, factoryName, name, integrationRuntime, ""); err != nil {
		return fmt.Errorf("Error creating/updating Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q): %+v", name, resourceGroup, factoryName, err)
	}

	resp, err := client.Get(ctx, resourceGroup, factoryName, name, "")
	if err != nil {
		return fmt.Errorf("Error retrieving Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q): %+v", name, resourceGroup, factoryName, err)
	}

	if resp.ID == nil {
		return fmt.Errorf("Cannot read Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q) ID", name, resourceGroup, factoryName)
	}

	d.SetId(*resp.ID)

	return resourceDataFactoryIntegrationRuntimeManagedRead(d, meta)
}

func resourceDataFactoryIntegrationRuntimeManagedRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.IntegrationRuntimesClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	factoryName := id.Path["factories"]
	name := id.Path["integrationruntimes"]

	resp, err := client.Get(ctx, resourceGroup, factoryName, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error retrieving Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q): %+v", name, resourceGroup, factoryName, err)
	}

	d.Set("name", name)
	d.Set("data_factory_name", factoryName)
	d.Set("resource_group_name", resourceGroup)

	managedIntegrationRuntime, convertSuccess := resp.Properties.AsManagedIntegrationRuntime()
	if !convertSuccess {
		return fmt.Errorf("Error converting integration runtime to managed managed integration runtime %q (Resource Group %q, Data Factory %q)", name, resourceGroup, factoryName)
	}

	if managedIntegrationRuntime.Description != nil {
		d.Set("description", managedIntegrationRuntime.Description)
	}

	if computeProps := managedIntegrationRuntime.ComputeProperties; computeProps != nil {
		if location := computeProps.Location; location != nil {
			d.Set("location", location)
		}

		if nodeSize := computeProps.NodeSize; nodeSize != nil {
			d.Set("node_size", nodeSize)
		}

		if numberOfNodes := computeProps.NumberOfNodes; numberOfNodes != nil {
			d.Set("number_of_nodes", numberOfNodes)
		}

		if maxParallelExecutionsPerNode := computeProps.MaxParallelExecutionsPerNode; maxParallelExecutionsPerNode != nil {
			d.Set("max_parallel_executions_per_node", maxParallelExecutionsPerNode)
		}

		if err := d.Set("vnet_integration", flattenDataFactoryIntegrationRuntimeManagedVnetIntegration(computeProps.VNetProperties)); err != nil {
			return fmt.Errorf("Error setting `vnet_integration`: %+v", err)
		}
	}

	if ssisProps := managedIntegrationRuntime.SsisProperties; ssisProps != nil {
		d.Set("edition", string(ssisProps.Edition))
		d.Set("license_type", string(ssisProps.LicenseType))

		if err := d.Set("catalog_info", flattenDataFactoryIntegrationRuntimeManagedSsisCatalogInfo(ssisProps.CatalogInfo, d)); err != nil {
			return fmt.Errorf("Error setting `vnet_integration`: %+v", err)
		}

		if err := d.Set("custom_setup_script", flattenDataFactoryIntegrationRuntimeManagedSsisCustomSetupScript(ssisProps.CustomSetupScriptProperties, d)); err != nil {
			return fmt.Errorf("Error setting `vnet_integration`: %+v", err)
		}
	}

	return nil
}

func resourceDataFactoryIntegrationRuntimeManagedDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DataFactory.IntegrationRuntimesClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	factoryName := id.Path["factories"]
	name := id.Path["integrationruntimes"]

	response, err := client.Delete(ctx, resourceGroup, factoryName, name)
	if err != nil {
		if !utils.ResponseWasNotFound(response) {
			return fmt.Errorf("Error deleting Data Factory Managed Integration Runtime %q (Resource Group %q, Data Factory %q): %+v", name, resourceGroup, factoryName, err)
		}
	}

	return nil
}

func expandDataFactoryIntegrationRuntimeManagedComputeProperties(d *pluginsdk.ResourceData) *datafactory.IntegrationRuntimeComputeProperties {
	location := azure.NormalizeLocation(d.Get("location").(string))
	computeProperties := datafactory.IntegrationRuntimeComputeProperties{
		Location:                     &location,
		NodeSize:                     utils.String(d.Get("node_size").(string)),
		NumberOfNodes:                utils.Int32(int32(d.Get("number_of_nodes").(int))),
		MaxParallelExecutionsPerNode: utils.Int32(int32(d.Get("max_parallel_executions_per_node").(int))),
	}

	if vnetIntegrations, ok := d.GetOk("vnet_integration"); ok && len(vnetIntegrations.([]interface{})) > 0 {
		vnetProps := vnetIntegrations.([]interface{})[0].(map[string]interface{})
		computeProperties.VNetProperties = &datafactory.IntegrationRuntimeVNetProperties{
			VNetID: utils.String(vnetProps["vnet_id"].(string)),
			Subnet: utils.String(vnetProps["subnet_name"].(string)),
		}
	}

	return &computeProperties
}

func expandDataFactoryIntegrationRuntimeManagedSsisProperties(d *pluginsdk.ResourceData) *datafactory.IntegrationRuntimeSsisProperties {
	ssisProperties := &datafactory.IntegrationRuntimeSsisProperties{
		Edition:     datafactory.IntegrationRuntimeEdition(d.Get("edition").(string)),
		LicenseType: datafactory.IntegrationRuntimeLicenseType(d.Get("license_type").(string)),
	}

	if catalogInfos, ok := d.GetOk("catalog_info"); ok && len(catalogInfos.([]interface{})) > 0 {
		catalogInfo := catalogInfos.([]interface{})[0].(map[string]interface{})

		ssisProperties.CatalogInfo = &datafactory.IntegrationRuntimeSsisCatalogInfo{
			CatalogServerEndpoint: utils.String(catalogInfo["server_endpoint"].(string)),
			CatalogPricingTier:    datafactory.IntegrationRuntimeSsisCatalogPricingTier(catalogInfo["pricing_tier"].(string)),
		}

		if adminUserName := catalogInfo["administrator_login"]; adminUserName.(string) != "" {
			ssisProperties.CatalogInfo.CatalogAdminUserName = utils.String(adminUserName.(string))
		}

		if adminPassword := catalogInfo["administrator_password"]; adminPassword.(string) != "" {
			ssisProperties.CatalogInfo.CatalogAdminPassword = &datafactory.SecureString{
				Value: utils.String(adminPassword.(string)),
				Type:  datafactory.TypeSecureString,
			}
		}
	}

	if customSetupScripts, ok := d.GetOk("custom_setup_script"); ok && len(customSetupScripts.([]interface{})) > 0 {
		customSetupScript := customSetupScripts.([]interface{})[0].(map[string]interface{})

		sasToken := &datafactory.SecureString{
			Value: utils.String(customSetupScript["sas_token"].(string)),
			Type:  datafactory.TypeSecureString,
		}

		ssisProperties.CustomSetupScriptProperties = &datafactory.IntegrationRuntimeCustomSetupScriptProperties{
			BlobContainerURI: utils.String(customSetupScript["blob_container_uri"].(string)),
			SasToken:         sasToken,
		}
	}

	return ssisProperties
}

func flattenDataFactoryIntegrationRuntimeManagedVnetIntegration(vnetProperties *datafactory.IntegrationRuntimeVNetProperties) []interface{} {
	if vnetProperties == nil {
		return []interface{}{}
	}

	return []interface{}{
		map[string]string{
			"vnet_id":     *vnetProperties.VNetID,
			"subnet_name": *vnetProperties.Subnet,
		},
	}
}

func flattenDataFactoryIntegrationRuntimeManagedSsisCatalogInfo(ssisProperties *datafactory.IntegrationRuntimeSsisCatalogInfo, d *pluginsdk.ResourceData) []interface{} {
	if ssisProperties == nil {
		return []interface{}{}
	}

	catalogInfo := map[string]string{
		"server_endpoint": *ssisProperties.CatalogServerEndpoint,
		"pricing_tier":    string(ssisProperties.CatalogPricingTier),
	}

	if ssisProperties.CatalogAdminUserName != nil {
		catalogInfo["administrator_login"] = *ssisProperties.CatalogAdminUserName
	}

	if adminPassword, ok := d.GetOk("catalog_info.0.administrator_password"); ok {
		catalogInfo["administrator_password"] = adminPassword.(string)
	}

	return []interface{}{catalogInfo}
}

func flattenDataFactoryIntegrationRuntimeManagedSsisCustomSetupScript(customSetupScriptProperties *datafactory.IntegrationRuntimeCustomSetupScriptProperties, d *pluginsdk.ResourceData) []interface{} {
	if customSetupScriptProperties == nil {
		return []interface{}{}
	}

	customSetupScript := map[string]string{
		"blob_container_uri": *customSetupScriptProperties.BlobContainerURI,
	}

	if sasToken, ok := d.GetOk("custom_setup_script.0.sas_token"); ok {
		customSetupScript["sas_token"] = sasToken.(string)
	}

	return []interface{}{customSetupScript}
}
