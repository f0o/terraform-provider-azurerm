package network

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2020-11-01/network"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/locks"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

const azureNetworkProfileResourceName = "azurerm_network_profile"

func resourceNetworkProfile() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceNetworkProfileCreateUpdate,
		Read:   resourceNetworkProfileRead,
		Update: resourceNetworkProfileCreateUpdate,
		Delete: resourceNetworkProfileDelete,
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
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"location": azure.SchemaLocation(),

			"resource_group_name": azure.SchemaResourceGroupName(),

			"container_network_interface": {
				Type:     pluginsdk.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"name": {
							Type:         pluginsdk.TypeString,
							Required:     true,
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"ip_configuration": {
							Type:     pluginsdk.TypeList,
							Required: true,
							Elem: &pluginsdk.Resource{
								Schema: map[string]*pluginsdk.Schema{
									"name": {
										Type:         pluginsdk.TypeString,
										Required:     true,
										ValidateFunc: validation.StringIsNotEmpty,
									},
									"subnet_id": {
										Type:         pluginsdk.TypeString,
										Required:     true,
										ValidateFunc: azure.ValidateResourceID,
									},
								},
							},
						},
					},
				},
			},

			"container_network_interface_ids": {
				Type:     pluginsdk.TypeList,
				Computed: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
				},
			},

			"tags": tags.Schema(),
		},
	}
}

func resourceNetworkProfileCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.ProfileClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for Network Profile creation")

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceGroup, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Network Profile %q (Resource Group %q): %s", name, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_network_profile", *existing.ID)
		}
	}

	location := azure.NormalizeLocation(d.Get("location").(string))
	t := d.Get("tags").(map[string]interface{})

	subnetsToLock, vnetsToLock, err := expandNetworkProfileVirtualNetworkSubnetNames(d)
	if err != nil {
		return fmt.Errorf("Error extracting names of Subnet and Virtual Network: %+v", err)
	}

	locks.ByName(name, azureNetworkProfileResourceName)
	defer locks.UnlockByName(name, azureNetworkProfileResourceName)

	locks.MultipleByName(vnetsToLock, VirtualNetworkResourceName)
	defer locks.UnlockMultipleByName(vnetsToLock, VirtualNetworkResourceName)

	locks.MultipleByName(subnetsToLock, SubnetResourceName)
	defer locks.UnlockMultipleByName(subnetsToLock, SubnetResourceName)

	parameters := network.Profile{
		Location: &location,
		Tags:     tags.Expand(t),
		ProfilePropertiesFormat: &network.ProfilePropertiesFormat{
			ContainerNetworkInterfaceConfigurations: expandNetworkProfileContainerNetworkInterface(d),
		},
	}

	if _, err := client.CreateOrUpdate(ctx, resourceGroup, name, parameters); err != nil {
		return fmt.Errorf("Error creating/updating Network Profile %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	profile, err := client.Get(ctx, resourceGroup, name, "")
	if err != nil {
		return fmt.Errorf("Error retrieving Network Profile %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	if profile.ID == nil {
		return fmt.Errorf("Cannot read Network Profile %q (Resource Group %q) ID", name, resourceGroup)
	}

	d.SetId(*profile.ID)

	return resourceNetworkProfileRead(d, meta)
}

func resourceNetworkProfileRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.ProfileClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["networkProfiles"]

	profile, err := client.Get(ctx, resourceGroup, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(profile.Response) {
			log.Printf("[DEBUG] Network Profile %q was not found in Resource Group %q - removing from state!", name, resourceGroup)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error making Read request on Network Profile %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	d.Set("name", profile.Name)
	d.Set("resource_group_name", resourceGroup)
	if location := profile.Location; location != nil {
		d.Set("location", azure.NormalizeLocation(*location))
	}

	if props := profile.ProfilePropertiesFormat; props != nil {
		cniConfigs := flattenNetworkProfileContainerNetworkInterface(props.ContainerNetworkInterfaceConfigurations)
		if err := d.Set("container_network_interface", cniConfigs); err != nil {
			return fmt.Errorf("Error setting `container_network_interface`: %+v", err)
		}

		cniIDs := flattenNetworkProfileContainerNetworkInterfaceIDs(props.ContainerNetworkInterfaces)
		if err := d.Set("container_network_interface_ids", cniIDs); err != nil {
			return fmt.Errorf("Error setting `container_network_interface_ids`: %+v", err)
		}
	}

	return tags.FlattenAndSet(d, profile.Tags)
}

func resourceNetworkProfileDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.ProfileClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["networkProfiles"]

	read, err := client.Get(ctx, resourceGroup, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			// deleted outside of TF
			log.Printf("[DEBUG] Network Profile %q was not found in Resource Group %q - assuming removed!", name, resourceGroup)
			return nil
		}

		return fmt.Errorf("Error retrieving Network Profile %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	subnetsToLock, vnetsToLock, err := expandNetworkProfileVirtualNetworkSubnetNames(d)
	if err != nil {
		return fmt.Errorf("Error extracting names of Subnet and Virtual Network: %+v", err)
	}

	locks.ByName(name, azureNetworkProfileResourceName)
	defer locks.UnlockByName(name, azureNetworkProfileResourceName)

	locks.MultipleByName(vnetsToLock, VirtualNetworkResourceName)
	defer locks.UnlockMultipleByName(vnetsToLock, VirtualNetworkResourceName)

	locks.MultipleByName(subnetsToLock, SubnetResourceName)
	defer locks.UnlockMultipleByName(subnetsToLock, SubnetResourceName)

	if _, err = client.Delete(ctx, resourceGroup, name); err != nil {
		return fmt.Errorf("Error deleting Network Profile %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	return err
}

func expandNetworkProfileContainerNetworkInterface(d *pluginsdk.ResourceData) *[]network.ContainerNetworkInterfaceConfiguration {
	cniConfigs := d.Get("container_network_interface").([]interface{})
	retCNIConfigs := make([]network.ContainerNetworkInterfaceConfiguration, 0)

	for _, cniConfig := range cniConfigs {
		nciData := cniConfig.(map[string]interface{})
		nciName := nciData["name"].(string)
		ipConfigs := nciData["ip_configuration"].([]interface{})

		retIPConfigs := make([]network.IPConfigurationProfile, 0)
		for _, ipConfig := range ipConfigs {
			ipData := ipConfig.(map[string]interface{})
			ipName := ipData["name"].(string)
			subNetID := ipData["subnet_id"].(string)

			retIPConfig := network.IPConfigurationProfile{
				Name: &ipName,
				IPConfigurationProfilePropertiesFormat: &network.IPConfigurationProfilePropertiesFormat{
					Subnet: &network.Subnet{
						ID: &subNetID,
					},
				},
			}

			retIPConfigs = append(retIPConfigs, retIPConfig)
		}

		retCNIConfig := network.ContainerNetworkInterfaceConfiguration{
			Name: &nciName,
			ContainerNetworkInterfaceConfigurationPropertiesFormat: &network.ContainerNetworkInterfaceConfigurationPropertiesFormat{
				IPConfigurations: &retIPConfigs,
			},
		}

		retCNIConfigs = append(retCNIConfigs, retCNIConfig)
	}

	return &retCNIConfigs
}

func expandNetworkProfileVirtualNetworkSubnetNames(d *pluginsdk.ResourceData) (*[]string, *[]string, error) {
	cniConfigs := d.Get("container_network_interface").([]interface{})
	subnetNames := make([]string, 0)
	vnetNames := make([]string, 0)

	for _, cniConfig := range cniConfigs {
		nciData := cniConfig.(map[string]interface{})
		ipConfigs := nciData["ip_configuration"].([]interface{})

		for _, ipConfig := range ipConfigs {
			ipData := ipConfig.(map[string]interface{})
			subnetID := ipData["subnet_id"].(string)

			subnetResourceID, err := azure.ParseAzureResourceID(subnetID)
			if err != nil {
				return nil, nil, err
			}

			subnetName := subnetResourceID.Path["subnets"]
			vnetName := subnetResourceID.Path["virtualNetworks"]

			if !utils.SliceContainsValue(subnetNames, subnetName) {
				subnetNames = append(subnetNames, subnetName)
			}

			if !utils.SliceContainsValue(vnetNames, vnetName) {
				vnetNames = append(vnetNames, vnetName)
			}
		}
	}

	return &subnetNames, &vnetNames, nil
}

func flattenNetworkProfileContainerNetworkInterface(input *[]network.ContainerNetworkInterfaceConfiguration) []interface{} {
	retCNIConfigs := make([]interface{}, 0)
	if input == nil {
		return retCNIConfigs
	}

	// if-continue is used to simplify the deeply nested if-else statement.
	for _, cniConfig := range *input {
		retCNIConfig := make(map[string]interface{})

		if cniConfig.Name != nil {
			retCNIConfig["name"] = *cniConfig.Name
		}

		retIPConfigs := make([]interface{}, 0)
		if cniProps := cniConfig.ContainerNetworkInterfaceConfigurationPropertiesFormat; cniProps != nil && cniProps.IPConfigurations != nil {
			for _, ipConfig := range *cniProps.IPConfigurations {
				retIPConfig := make(map[string]interface{})

				if ipConfig.Name != nil {
					retIPConfig["name"] = *ipConfig.Name
				}

				if ipProps := ipConfig.IPConfigurationProfilePropertiesFormat; ipProps != nil && ipProps.Subnet != nil && ipProps.Subnet.ID != nil {
					retIPConfig["subnet_id"] = *ipProps.Subnet.ID
				}

				retIPConfigs = append(retIPConfigs, retIPConfig)
			}
		}
		retCNIConfig["ip_configuration"] = retIPConfigs

		retCNIConfigs = append(retCNIConfigs, retCNIConfig)
	}

	return retCNIConfigs
}

func flattenNetworkProfileContainerNetworkInterfaceIDs(input *[]network.ContainerNetworkInterface) []string {
	retCNIs := make([]string, 0)
	if input == nil {
		return retCNIs
	}

	for _, retCNI := range *input {
		if retCNI.ID != nil {
			retCNIs = append(retCNIs, *retCNI.ID)
		}
	}

	return retCNIs
}
