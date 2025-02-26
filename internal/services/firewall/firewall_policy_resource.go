package firewall

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2020-11-01/network"
	"github.com/hashicorp/go-azure-helpers/response"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/location"
	"github.com/hashicorp/terraform-provider-azurerm/internal/locks"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/firewall/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/firewall/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

const azureFirewallPolicyResourceName = "azurerm_firewall_policy"

func resourceFirewallPolicy() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceFirewallPolicyCreateUpdate,
		Read:   resourceFirewallPolicyRead,
		Update: resourceFirewallPolicyCreateUpdate,
		Delete: resourceFirewallPolicyDelete,

		Importer: pluginsdk.ImporterValidatingResourceId(func(id string) error {
			_, err := parse.FirewallPolicyID(id)
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
				ValidateFunc: validate.FirewallPolicyName(),
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"sku": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					string(network.FirewallPolicySkuTierPremium),
					string(network.FirewallPolicySkuTierStandard),
				}, false),
			},

			"location": location.Schema(),

			"base_policy_id": {
				Type:         pluginsdk.TypeString,
				Optional:     true,
				ValidateFunc: validate.FirewallPolicyID,
			},

			"dns": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				MinItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"servers": {
							Type:     pluginsdk.TypeSet,
							Optional: true,
							Elem: &pluginsdk.Schema{
								Type:         pluginsdk.TypeString,
								ValidateFunc: validation.IsIPv4Address,
							},
						},
						"proxy_enabled": {
							Type:     pluginsdk.TypeBool,
							Optional: true,
							Default:  false,
						},
						// TODO 3.0 - remove this property
						"network_rule_fqdn_enabled": {
							Type:       pluginsdk.TypeBool,
							Optional:   true,
							Computed:   true,
							Deprecated: "This property has been deprecated as the service team has removed it from all API versions and is no longer supported by Azure. It will be removed in v3.0 of the provider.",
						},
					},
				},
			},

			"threat_intelligence_mode": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Default:  string(network.AzureFirewallThreatIntelModeAlert),
				ValidateFunc: validation.StringInSlice([]string{
					string(network.AzureFirewallThreatIntelModeAlert),
					string(network.AzureFirewallThreatIntelModeDeny),
					string(network.AzureFirewallThreatIntelModeOff),
				}, false),
			},

			"threat_intelligence_allowlist": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MaxItems: 1,
				MinItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"ip_addresses": {
							Type:     pluginsdk.TypeSet,
							Optional: true,
							Elem: &pluginsdk.Schema{
								Type:         pluginsdk.TypeString,
								ValidateFunc: validation.Any(validation.IsIPv4Range, validation.IsIPv4Address),
							},
							AtLeastOneOf: []string{"threat_intelligence_allowlist.0.ip_addresses", "threat_intelligence_allowlist.0.fqdns"},
						},
						"fqdns": {
							Type:     pluginsdk.TypeSet,
							Optional: true,
							Elem: &pluginsdk.Schema{
								Type:         pluginsdk.TypeString,
								ValidateFunc: validation.StringIsNotEmpty,
							},
							AtLeastOneOf: []string{"threat_intelligence_allowlist.0.ip_addresses", "threat_intelligence_allowlist.0.fqdns"},
						},
					},
				},
			},

			"child_policies": {
				Type:     pluginsdk.TypeList,
				Computed: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
				},
			},

			"firewalls": {
				Type:     pluginsdk.TypeList,
				Computed: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
				},
			},

			"rule_collection_groups": {
				Type:     pluginsdk.TypeList,
				Computed: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
				},
			},

			"private_ip_ranges": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				MinItems: 1,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
					ValidateFunc: validation.Any(
						validation.IsCIDR,
						validation.IsIPv4Address,
					),
				},
			},

			"tags": tags.SchemaEnforceLowerCaseKeys(),
		},
	}
}

func resourceFirewallPolicyCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Firewall.FirewallPolicyClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		resp, err := client.Get(ctx, resourceGroup, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(resp.Response) {
				return fmt.Errorf("checking for existing Firewall Policy %q (Resource Group %q): %+v", name, resourceGroup, err)
			}
		}

		if resp.ID != nil && *resp.ID != "" {
			return tf.ImportAsExistsError("azurerm_firewall_policy", *resp.ID)
		}
	}

	props := network.FirewallPolicy{
		FirewallPolicyPropertiesFormat: &network.FirewallPolicyPropertiesFormat{
			ThreatIntelMode:      network.AzureFirewallThreatIntelMode(d.Get("threat_intelligence_mode").(string)),
			ThreatIntelWhitelist: expandFirewallPolicyThreatIntelWhitelist(d.Get("threat_intelligence_allowlist").([]interface{})),
			DNSSettings:          expandFirewallPolicyDNSSetting(d.Get("dns").([]interface{})),
		},
		Location: utils.String(location.Normalize(d.Get("location").(string))),
		Tags:     tags.Expand(d.Get("tags").(map[string]interface{})),
	}
	if id, ok := d.GetOk("base_policy_id"); ok {
		props.FirewallPolicyPropertiesFormat.BasePolicy = &network.SubResource{ID: utils.String(id.(string))}
	}

	if v, ok := d.GetOk("sku"); ok {
		props.FirewallPolicyPropertiesFormat.Sku = &network.FirewallPolicySku{
			Tier: network.FirewallPolicySkuTier(v.(string)),
		}
	}

	if v, ok := d.GetOk("private_ip_ranges"); ok {
		privateIpRanges := utils.ExpandStringSlice(v.([]interface{}))
		props.FirewallPolicyPropertiesFormat.Snat = &network.FirewallPolicySNAT{
			PrivateRanges: privateIpRanges,
		}
	}

	locks.ByName(name, azureFirewallPolicyResourceName)
	defer locks.UnlockByName(name, azureFirewallPolicyResourceName)

	if _, err := client.CreateOrUpdate(ctx, resourceGroup, name, props); err != nil {
		return fmt.Errorf("creating Firewall Policy %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	resp, err := client.Get(ctx, resourceGroup, name, "")
	if err != nil {
		return fmt.Errorf("retrieving Firewall Policy %q (Resource Group %q): %+v", name, resourceGroup, err)
	}
	if resp.ID == nil || *resp.ID == "" {
		return fmt.Errorf("empty or nil ID returned for Firewall Policy %q (Resource Group %q) ID", name, resourceGroup)
	}
	d.SetId(*resp.ID)

	return resourceFirewallPolicyRead(d, meta)
}

func resourceFirewallPolicyRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Firewall.FirewallPolicyClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.FirewallPolicyID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.Name, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] Firewall Policy %q was not found in Resource Group %q - removing from state!", id.Name, id.ResourceGroup)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("retrieving Firewall Policy %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	d.Set("name", id.Name)
	d.Set("resource_group_name", id.ResourceGroup)
	d.Set("location", location.NormalizeNilable(resp.Location))

	if prop := resp.FirewallPolicyPropertiesFormat; prop != nil {
		basePolicyID := ""
		if resp.BasePolicy != nil && resp.BasePolicy.ID != nil {
			basePolicyID = *resp.BasePolicy.ID
		}
		d.Set("base_policy_id", basePolicyID)

		d.Set("threat_intelligence_mode", string(prop.ThreatIntelMode))

		if sku := prop.Sku; sku != nil {
			d.Set("sku", string(sku.Tier))
		}

		if err := d.Set("threat_intelligence_allowlist", flattenFirewallPolicyThreatIntelWhitelist(resp.ThreatIntelWhitelist)); err != nil {
			return fmt.Errorf(`setting "threat_intelligence_allowlist": %+v`, err)
		}

		if err := d.Set("dns", flattenFirewallPolicyDNSSetting(prop.DNSSettings)); err != nil {
			return fmt.Errorf(`setting "dns": %+v`, err)
		}

		if err := d.Set("child_policies", flattenNetworkSubResourceID(prop.ChildPolicies)); err != nil {
			return fmt.Errorf(`setting "child_policies": %+v`, err)
		}

		if err := d.Set("firewalls", flattenNetworkSubResourceID(prop.Firewalls)); err != nil {
			return fmt.Errorf(`setting "firewalls": %+v`, err)
		}

		if err := d.Set("rule_collection_groups", flattenNetworkSubResourceID(prop.RuleCollectionGroups)); err != nil {
			return fmt.Errorf(`setting "rule_collection_groups": %+v`, err)
		}

		var privateIpRanges []interface{}
		if prop.Snat != nil {
			privateIpRanges = utils.FlattenStringSlice(prop.Snat.PrivateRanges)
		}
		if err := d.Set("private_ip_ranges", privateIpRanges); err != nil {
			return fmt.Errorf("Error setting `private_ip_ranges`: %+v", err)
		}
	}

	return tags.FlattenAndSet(d, resp.Tags)
}

func resourceFirewallPolicyDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Firewall.FirewallPolicyClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.FirewallPolicyID(d.Id())
	if err != nil {
		return err
	}

	locks.ByName(id.Name, azureFirewallPolicyResourceName)
	defer locks.UnlockByName(id.Name, azureFirewallPolicyResourceName)

	future, err := client.Delete(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		return fmt.Errorf("deleting Firewall Policy %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}
	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		if !response.WasNotFound(future.Response()) {
			return fmt.Errorf("waiting for deleting Firewall Policy %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
		}
	}

	return nil
}

func expandFirewallPolicyThreatIntelWhitelist(input []interface{}) *network.FirewallPolicyThreatIntelWhitelist {
	if len(input) == 0 || input[0] == nil {
		return nil
	}

	raw := input[0].(map[string]interface{})
	output := &network.FirewallPolicyThreatIntelWhitelist{
		IPAddresses: utils.ExpandStringSlice(raw["ip_addresses"].(*pluginsdk.Set).List()),
		Fqdns:       utils.ExpandStringSlice(raw["fqdns"].(*pluginsdk.Set).List()),
	}

	return output
}

func expandFirewallPolicyDNSSetting(input []interface{}) *network.DNSSettings {
	if len(input) == 0 || input[0] == nil {
		return nil
	}

	raw := input[0].(map[string]interface{})
	output := &network.DNSSettings{
		Servers:     utils.ExpandStringSlice(raw["servers"].(*pluginsdk.Set).List()),
		EnableProxy: utils.Bool(raw["proxy_enabled"].(bool)),
	}

	return output
}

func flattenFirewallPolicyThreatIntelWhitelist(input *network.FirewallPolicyThreatIntelWhitelist) []interface{} {
	if input == nil {
		return []interface{}{}
	}

	return []interface{}{
		map[string]interface{}{
			"ip_addresses": utils.FlattenStringSlice(input.IPAddresses),
			"fqdns":        utils.FlattenStringSlice(input.Fqdns),
		},
	}
}

func flattenFirewallPolicyDNSSetting(input *network.DNSSettings) []interface{} {
	if input == nil {
		return []interface{}{}
	}

	proxyEnabled := false
	if input.EnableProxy != nil {
		proxyEnabled = *input.EnableProxy
	}

	return []interface{}{
		map[string]interface{}{
			"servers":       utils.FlattenStringSlice(input.Servers),
			"proxy_enabled": proxyEnabled,
			// TODO 3.0: remove the setting zero value for property below.
			"network_rule_fqdn_enabled": false,
		},
	}
}
