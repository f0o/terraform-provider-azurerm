package network

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2020-11-01/network"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/locks"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceNetworkInterfaceBackendAddressPoolAssociation() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceNetworkInterfaceBackendAddressPoolAssociationCreate,
		Read:   resourceNetworkInterfaceBackendAddressPoolAssociationRead,
		Delete: resourceNetworkInterfaceBackendAddressPoolAssociationDelete,
		// TODO: replace this with an importer which validates the ID during import
		Importer: pluginsdk.DefaultImporter(),

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*pluginsdk.Schema{
			"network_interface_id": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: azure.ValidateResourceID,
			},

			"ip_configuration_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"backend_address_pool_id": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: azure.ValidateResourceID,
			},
		},
	}
}

func resourceNetworkInterfaceBackendAddressPoolAssociationCreate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.InterfacesClient
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for Network Interface <-> Load Balancer Backend Address Pool Association creation.")

	networkInterfaceId := d.Get("network_interface_id").(string)
	ipConfigurationName := d.Get("ip_configuration_name").(string)
	backendAddressPoolId := d.Get("backend_address_pool_id").(string)

	id, err := azure.ParseAzureResourceID(networkInterfaceId)
	if err != nil {
		return err
	}

	networkInterfaceName := id.Path["networkInterfaces"]
	resourceGroup := id.ResourceGroup

	locks.ByName(networkInterfaceName, networkInterfaceResourceName)
	defer locks.UnlockByName(networkInterfaceName, networkInterfaceResourceName)

	read, err := client.Get(ctx, resourceGroup, networkInterfaceName, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			return fmt.Errorf("Network Interface %q (Resource Group %q) was not found!", networkInterfaceName, resourceGroup)
		}

		return fmt.Errorf("Error retrieving Network Interface %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	props := read.InterfacePropertiesFormat
	if props == nil {
		return fmt.Errorf("Error: `properties` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	ipConfigs := props.IPConfigurations
	if ipConfigs == nil {
		return fmt.Errorf("Error: `properties.IPConfigurations` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	c := FindNetworkInterfaceIPConfiguration(props.IPConfigurations, ipConfigurationName)
	if c == nil {
		return fmt.Errorf("Error: IP Configuration %q was not found on Network Interface %q (Resource Group %q)", ipConfigurationName, networkInterfaceName, resourceGroup)
	}

	config := *c
	p := config.InterfaceIPConfigurationPropertiesFormat
	if p == nil {
		return fmt.Errorf("Error: `IPConfiguration.properties` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	pools := make([]network.BackendAddressPool, 0)

	// first double-check it doesn't exist
	resourceId := fmt.Sprintf("%s/ipConfigurations/%s|%s", networkInterfaceId, ipConfigurationName, backendAddressPoolId)
	if p.LoadBalancerBackendAddressPools != nil {
		for _, existingPool := range *p.LoadBalancerBackendAddressPools {
			if id := existingPool.ID; id != nil {
				if *id == backendAddressPoolId {
					return tf.ImportAsExistsError("azurerm_network_interface_backend_address_pool_association", resourceId)
				}

				pools = append(pools, existingPool)
			}
		}
	}

	pool := network.BackendAddressPool{
		ID: utils.String(backendAddressPoolId),
	}
	pools = append(pools, pool)
	p.LoadBalancerBackendAddressPools = &pools

	props.IPConfigurations = updateNetworkInterfaceIPConfiguration(config, props.IPConfigurations)

	future, err := client.CreateOrUpdate(ctx, resourceGroup, networkInterfaceName, read)
	if err != nil {
		return fmt.Errorf("Error updating Backend Address Pool Association for Network Interface %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for completion of Backend Address Pool Association for NIC %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	d.SetId(resourceId)

	return resourceNetworkInterfaceBackendAddressPoolAssociationRead(d, meta)
}

func resourceNetworkInterfaceBackendAddressPoolAssociationRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.InterfacesClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	splitId := strings.Split(d.Id(), "|")
	if len(splitId) != 2 {
		return fmt.Errorf("Expected ID to be in the format {networkInterfaceId}/ipConfigurations/{ipConfigurationName}|{backendAddressPoolId} but got %q", d.Id())
	}

	nicID, err := azure.ParseAzureResourceID(splitId[0])
	if err != nil {
		return err
	}

	ipConfigurationName := nicID.Path["ipConfigurations"]
	networkInterfaceName := nicID.Path["networkInterfaces"]
	resourceGroup := nicID.ResourceGroup
	backendAddressPoolId := splitId[1]

	read, err := client.Get(ctx, resourceGroup, networkInterfaceName, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			log.Printf("Network Interface %q (Resource Group %q) was not found - removing from state!", networkInterfaceName, resourceGroup)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error retrieving Network Interface %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	nicProps := read.InterfacePropertiesFormat
	if nicProps == nil {
		return fmt.Errorf("Error: `properties` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	ipConfigs := nicProps.IPConfigurations
	if ipConfigs == nil {
		return fmt.Errorf("Error: `properties.IPConfigurations` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	c := FindNetworkInterfaceIPConfiguration(nicProps.IPConfigurations, ipConfigurationName)
	if c == nil {
		log.Printf("IP Configuration %q was not found in Network Interface %q (Resource Group %q) - removing from state!", ipConfigurationName, networkInterfaceName, resourceGroup)
		d.SetId("")
		return nil
	}
	config := *c

	found := false
	if props := config.InterfaceIPConfigurationPropertiesFormat; props != nil {
		if backendPools := props.LoadBalancerBackendAddressPools; backendPools != nil {
			for _, pool := range *backendPools {
				if pool.ID == nil {
					continue
				}

				if *pool.ID == backendAddressPoolId {
					found = true
					break
				}
			}
		}
	}

	if !found {
		log.Printf("[DEBUG] Association between Network Interface %q (Resource Group %q) and Load Balancer Backend Pool %q was not found - removing from state!", networkInterfaceName, resourceGroup, backendAddressPoolId)
		d.SetId("")
		return nil
	}

	d.Set("backend_address_pool_id", backendAddressPoolId)
	d.Set("ip_configuration_name", ipConfigurationName)
	d.Set("network_interface_id", read.ID)

	return nil
}

func resourceNetworkInterfaceBackendAddressPoolAssociationDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Network.InterfacesClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	splitId := strings.Split(d.Id(), "|")
	if len(splitId) != 2 {
		return fmt.Errorf("Expected ID to be in the format {networkInterfaceId}/ipConfigurations/{ipConfigurationName}|{backendAddressPoolId} but got %q", d.Id())
	}

	nicID, err := azure.ParseAzureResourceID(splitId[0])
	if err != nil {
		return err
	}

	ipConfigurationName := nicID.Path["ipConfigurations"]
	networkInterfaceName := nicID.Path["networkInterfaces"]
	resourceGroup := nicID.ResourceGroup
	backendAddressPoolId := splitId[1]

	locks.ByName(networkInterfaceName, networkInterfaceResourceName)
	defer locks.UnlockByName(networkInterfaceName, networkInterfaceResourceName)

	read, err := client.Get(ctx, resourceGroup, networkInterfaceName, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			return fmt.Errorf("Network Interface %q (Resource Group %q) was not found!", networkInterfaceName, resourceGroup)
		}

		return fmt.Errorf("Error retrieving Network Interface %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	nicProps := read.InterfacePropertiesFormat
	if nicProps == nil {
		return fmt.Errorf("Error: `properties` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	ipConfigs := nicProps.IPConfigurations
	if ipConfigs == nil {
		return fmt.Errorf("Error: `properties.IPConfigurations` was nil for Network Interface %q (Resource Group %q)", networkInterfaceName, resourceGroup)
	}

	c := FindNetworkInterfaceIPConfiguration(nicProps.IPConfigurations, ipConfigurationName)
	if c == nil {
		return fmt.Errorf("Error: IP Configuration %q was not found on Network Interface %q (Resource Group %q)", ipConfigurationName, networkInterfaceName, resourceGroup)
	}
	config := *c

	props := config.InterfaceIPConfigurationPropertiesFormat
	if props == nil {
		return fmt.Errorf("Error: Properties for IPConfiguration %q was nil for Network Interface %q (Resource Group %q)", ipConfigurationName, networkInterfaceName, resourceGroup)
	}

	backendAddressPools := make([]network.BackendAddressPool, 0)
	if backendPools := props.LoadBalancerBackendAddressPools; backendPools != nil {
		for _, pool := range *backendPools {
			if pool.ID == nil {
				continue
			}

			if *pool.ID != backendAddressPoolId {
				backendAddressPools = append(backendAddressPools, pool)
			}
		}
	}
	props.LoadBalancerBackendAddressPools = &backendAddressPools
	nicProps.IPConfigurations = updateNetworkInterfaceIPConfiguration(config, nicProps.IPConfigurations)

	future, err := client.CreateOrUpdate(ctx, resourceGroup, networkInterfaceName, read)
	if err != nil {
		return fmt.Errorf("Error removing Backend Address Pool Association for Network Interface %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for removal of Backend Address Pool Association for NIC %q (Resource Group %q): %+v", networkInterfaceName, resourceGroup, err)
	}

	return nil
}
