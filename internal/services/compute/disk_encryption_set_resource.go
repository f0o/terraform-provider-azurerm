package compute

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-12-01/compute"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/compute/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/compute/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/keyvault/client"
	keyVaultParse "github.com/hashicorp/terraform-provider-azurerm/internal/services/keyvault/parse"
	keyVaultValidate "github.com/hashicorp/terraform-provider-azurerm/internal/services/keyvault/validate"
	resourcesClient "github.com/hashicorp/terraform-provider-azurerm/internal/services/resource/client"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceDiskEncryptionSet() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceDiskEncryptionSetCreate,
		Read:   resourceDiskEncryptionSetRead,
		Update: resourceDiskEncryptionSetUpdate,
		Delete: resourceDiskEncryptionSetDelete,

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(60 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(60 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(60 * time.Minute),
		},

		Importer: pluginsdk.ImporterValidatingResourceId(func(id string) error {
			_, err := parse.DiskEncryptionSetID(id)
			return err
		}),

		Schema: map[string]*pluginsdk.Schema{
			"name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.DiskEncryptionSetName,
			},

			"location": azure.SchemaLocation(),

			"resource_group_name": azure.SchemaResourceGroupName(),

			"key_vault_key_id": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ValidateFunc: keyVaultValidate.NestedItemId,
			},

			"identity": {
				Type: pluginsdk.TypeList,
				// whilst the API Documentation shows optional - attempting to send nothing returns:
				// `Required parameter 'ResourceIdentity' is missing (null)`
				// hence this is required
				Required: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"type": {
							Type:     pluginsdk.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(compute.DiskEncryptionSetIdentityTypeSystemAssigned),
							}, false),
						},
						"principal_id": {
							Type:     pluginsdk.TypeString,
							Computed: true,
						},
						"tenant_id": {
							Type:     pluginsdk.TypeString,
							Computed: true,
						},
					},
				},
			},

			"tags": tags.Schema(),
		},
	}
}

func resourceDiskEncryptionSetCreate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Compute.DiskEncryptionSetsClient
	keyVaultsClient := meta.(*clients.Client).KeyVault
	resourcesClient := meta.(*clients.Client).Resource
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	existing, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		if !utils.ResponseWasNotFound(existing.Response) {
			return fmt.Errorf("Error checking for present of existing Disk Encryption Set %q (Resource Group %q): %+v", name, resourceGroup, err)
		}
	}
	if existing.ID != nil && *existing.ID != "" {
		return tf.ImportAsExistsError("azurerm_disk_encryption_set", *existing.ID)
	}

	keyVaultKeyId := d.Get("key_vault_key_id").(string)
	keyVaultDetails, err := diskEncryptionSetRetrieveKeyVault(ctx, keyVaultsClient, resourcesClient, keyVaultKeyId)
	if err != nil {
		return fmt.Errorf("Error validating Key Vault Key %q for Disk Encryption Set: %+v", keyVaultKeyId, err)
	}
	if !keyVaultDetails.softDeleteEnabled {
		return fmt.Errorf("Error validating Key Vault %q (Resource Group %q) for Disk Encryption Set: Soft Delete must be enabled but it isn't!", keyVaultDetails.keyVaultName, keyVaultDetails.resourceGroupName)
	}
	if !keyVaultDetails.purgeProtectionEnabled {
		return fmt.Errorf("Error validating Key Vault %q (Resource Group %q) for Disk Encryption Set: Purge Protection must be enabled but it isn't!", keyVaultDetails.keyVaultName, keyVaultDetails.resourceGroupName)
	}

	location := azure.NormalizeLocation(d.Get("location").(string))
	identityRaw := d.Get("identity").([]interface{})
	t := d.Get("tags").(map[string]interface{})

	params := compute.DiskEncryptionSet{
		Location: utils.String(location),
		EncryptionSetProperties: &compute.EncryptionSetProperties{
			ActiveKey: &compute.KeyForDiskEncryptionSet{
				KeyURL: utils.String(keyVaultKeyId),
				SourceVault: &compute.SourceVault{
					ID: utils.String(keyVaultDetails.keyVaultId),
				},
			},
		},
		Identity: expandDiskEncryptionSetIdentity(identityRaw),
		Tags:     tags.Expand(t),
	}

	future, err := client.CreateOrUpdate(ctx, resourceGroup, name, params)
	if err != nil {
		return fmt.Errorf("Error creating Disk Encryption Set %q (Resource Group %q): %+v", name, resourceGroup, err)
	}
	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for creation of Disk Encryption Set %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	resp, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		return fmt.Errorf("Error retrieving Disk Encryption Set %q (Resource Group %q): %+v", name, resourceGroup, err)
	}
	if resp.ID == nil {
		return fmt.Errorf("Cannot read Disk Encryption Set %q (Resource Group %q) ID", name, resourceGroup)
	}
	d.SetId(*resp.ID)

	return resourceDiskEncryptionSetRead(d, meta)
}

func resourceDiskEncryptionSetRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Compute.DiskEncryptionSetsClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.DiskEncryptionSetID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[INFO] Disk Encryption Set %q does not exist - removing from state", d.Id())
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error reading Disk Encryption Set %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	d.Set("name", id.Name)
	d.Set("resource_group_name", id.ResourceGroup)
	if location := resp.Location; location != nil {
		d.Set("location", azure.NormalizeLocation(*location))
	}

	if props := resp.EncryptionSetProperties; props != nil {
		keyVaultKeyId := ""
		if props.ActiveKey != nil && props.ActiveKey.KeyURL != nil {
			keyVaultKeyId = *props.ActiveKey.KeyURL
		}
		d.Set("key_vault_key_id", keyVaultKeyId)
	}

	if err := d.Set("identity", flattenDiskEncryptionSetIdentity(resp.Identity)); err != nil {
		return fmt.Errorf("Error setting `identity`: %+v", err)
	}

	return tags.FlattenAndSet(d, resp.Tags)
}

func resourceDiskEncryptionSetUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Compute.DiskEncryptionSetsClient
	keyVaultsClient := meta.(*clients.Client).KeyVault
	resourcesClient := meta.(*clients.Client).Resource
	ctx, cancel := timeouts.ForUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.DiskEncryptionSetID(d.Id())
	if err != nil {
		return err
	}

	update := compute.DiskEncryptionSetUpdate{}
	if d.HasChange("tags") {
		update.Tags = tags.Expand(d.Get("tags").(map[string]interface{}))
	}

	if d.HasChange("key_vault_key_id") {
		keyVaultKeyId := d.Get("key_vault_key_id").(string)
		keyVaultDetails, err := diskEncryptionSetRetrieveKeyVault(ctx, keyVaultsClient, resourcesClient, keyVaultKeyId)
		if err != nil {
			return fmt.Errorf("Error validating Key Vault Key %q for Disk Encryption Set: %+v", keyVaultKeyId, err)
		}
		if !keyVaultDetails.softDeleteEnabled {
			return fmt.Errorf("Error validating Key Vault %q (Resource Group %q) for Disk Encryption Set: Soft Delete must be enabled but it isn't!", keyVaultDetails.keyVaultName, keyVaultDetails.resourceGroupName)
		}
		if !keyVaultDetails.purgeProtectionEnabled {
			return fmt.Errorf("Error validating Key Vault %q (Resource Group %q) for Disk Encryption Set: Purge Protection must be enabled but it isn't!", keyVaultDetails.keyVaultName, keyVaultDetails.resourceGroupName)
		}
		update.DiskEncryptionSetUpdateProperties = &compute.DiskEncryptionSetUpdateProperties{
			ActiveKey: &compute.KeyForDiskEncryptionSet{
				KeyURL: utils.String(keyVaultKeyId),
				SourceVault: &compute.SourceVault{
					ID: utils.String(keyVaultDetails.keyVaultId),
				},
			},
		}
	}

	future, err := client.Update(ctx, id.ResourceGroup, id.Name, update)
	if err != nil {
		return fmt.Errorf("Error updating Disk Encryption Set %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}
	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for update of Disk Encryption Set %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	return resourceDiskEncryptionSetRead(d, meta)
}

func resourceDiskEncryptionSetDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Compute.DiskEncryptionSetsClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.DiskEncryptionSetID(d.Id())
	if err != nil {
		return err
	}

	future, err := client.Delete(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		return fmt.Errorf("Error deleting Disk Encryption Set %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for deleting Disk Encryption Set %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	return nil
}

func expandDiskEncryptionSetIdentity(input []interface{}) *compute.EncryptionSetIdentity {
	val := input[0].(map[string]interface{})
	return &compute.EncryptionSetIdentity{
		Type: compute.DiskEncryptionSetIdentityType(val["type"].(string)),
	}
}

func flattenDiskEncryptionSetIdentity(input *compute.EncryptionSetIdentity) []interface{} {
	if input == nil {
		return make([]interface{}, 0)
	}

	identityType := string(input.Type)
	principalId := ""
	if input.PrincipalID != nil {
		principalId = *input.PrincipalID
	}
	tenantId := ""
	if input.TenantID != nil {
		tenantId = *input.TenantID
	}

	return []interface{}{
		map[string]interface{}{
			"type":         identityType,
			"principal_id": principalId,
			"tenant_id":    tenantId,
		},
	}
}

type diskEncryptionSetKeyVault struct {
	keyVaultId             string
	resourceGroupName      string
	keyVaultName           string
	purgeProtectionEnabled bool
	softDeleteEnabled      bool
}

func diskEncryptionSetRetrieveKeyVault(ctx context.Context, keyVaultsClient *client.Client, resourcesClient *resourcesClient.Client, id string) (*diskEncryptionSetKeyVault, error) {
	keyVaultKeyId, err := keyVaultParse.ParseNestedItemID(id)
	if err != nil {
		return nil, err
	}
	keyVaultID, err := keyVaultsClient.KeyVaultIDFromBaseUrl(ctx, resourcesClient, keyVaultKeyId.KeyVaultBaseUrl)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving the Resource ID the Key Vault at URL %q: %s", keyVaultKeyId.KeyVaultBaseUrl, err)
	}
	if keyVaultID == nil {
		return nil, fmt.Errorf("Unable to determine the Resource ID for the Key Vault at URL %q", keyVaultKeyId.KeyVaultBaseUrl)
	}

	parsedKeyVaultID, err := keyVaultParse.VaultID(*keyVaultID)
	if err != nil {
		return nil, err
	}

	resp, err := keyVaultsClient.VaultsClient.Get(ctx, parsedKeyVaultID.ResourceGroup, parsedKeyVaultID.Name)
	if err != nil {
		return nil, fmt.Errorf("retrieving %s: %+v", *parsedKeyVaultID, err)
	}

	purgeProtectionEnabled := false
	softDeleteEnabled := false

	if props := resp.Properties; props != nil {
		if props.EnableSoftDelete != nil {
			softDeleteEnabled = *props.EnableSoftDelete
		}

		if props.EnablePurgeProtection != nil {
			purgeProtectionEnabled = *props.EnablePurgeProtection
		}
	}

	return &diskEncryptionSetKeyVault{
		keyVaultId:             *keyVaultID,
		resourceGroupName:      parsedKeyVaultID.ResourceGroup,
		keyVaultName:           parsedKeyVaultID.Name,
		purgeProtectionEnabled: purgeProtectionEnabled,
		softDeleteEnabled:      softDeleteEnabled,
	}, nil
}
