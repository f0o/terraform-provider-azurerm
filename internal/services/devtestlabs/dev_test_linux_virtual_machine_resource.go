package devtestlabs

import (
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/devtestlabs/mgmt/2016-05-15/dtl"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/devtestlabs/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceArmDevTestLinuxVirtualMachine() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceArmDevTestLinuxVirtualMachineCreateUpdate,
		Read:   resourceArmDevTestLinuxVirtualMachineRead,
		Update: resourceArmDevTestLinuxVirtualMachineCreateUpdate,
		Delete: resourceArmDevTestLinuxVirtualMachineDelete,
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
				ValidateFunc: validate.DevTestVirtualMachineName(62),
			},

			"lab_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.DevTestLabName(),
			},

			// There's a bug in the Azure API where this is returned in lower-case
			// BUG: https://github.com/Azure/azure-rest-api-specs/issues/3964
			"resource_group_name": azure.SchemaResourceGroupNameDiffSuppress(),

			"location": azure.SchemaLocation(),

			"size": {
				Type:     pluginsdk.TypeString,
				Required: true,
				// since this isn't returned from the API
				ForceNew: true,
			},

			"username": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ForceNew: true,
			},

			"storage_type": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringInSlice([]string{
					"Standard",
					"Premium",
				}, false),
			},

			"lab_subnet_name": {
				Type:     pluginsdk.TypeString,
				Required: true,
				// since this isn't returned from the API
				ForceNew: true,
			},

			"lab_virtual_network_id": {
				Type:     pluginsdk.TypeString,
				Required: true,
				// since this isn't returned from the API
				ForceNew: true,
			},

			"allow_claim": {
				Type:     pluginsdk.TypeBool,
				Optional: true,
				Default:  true,
			},

			"disallow_public_ip_address": {
				Type:     pluginsdk.TypeBool,
				Optional: true,
				ForceNew: true,
			},

			"password": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				// since this isn't returned from the API
				ForceNew:  true,
				Sensitive: true,
			},

			"ssh_key": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				// since this isn't returned from the API
				ForceNew: true,
			},

			"gallery_image_reference": schemaDevTestVirtualMachineGalleryImageReference(),

			"inbound_nat_rule": schemaDevTestVirtualMachineInboundNatRule(),

			"notes": {
				Type:     pluginsdk.TypeString,
				Optional: true,
			},

			"tags": tags.Schema(),

			"fqdn": {
				Type:     pluginsdk.TypeString,
				Computed: true,
			},

			"unique_identifier": {
				Type:     pluginsdk.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceArmDevTestLinuxVirtualMachineCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DevTestLabs.VirtualMachinesClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for DevTest Linux Virtual Machine creation")

	name := d.Get("name").(string)
	labName := d.Get("lab_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resourceGroup, labName, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Linux Virtual Machine %q (Lab %q / Resource Group %q): %s", name, labName, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_dev_test_linux_virtual_machine", *existing.ID)
		}
	}

	t := d.Get("tags").(map[string]interface{})

	allowClaim := d.Get("allow_claim").(bool)
	disallowPublicIPAddress := d.Get("disallow_public_ip_address").(bool)
	labSubnetName := d.Get("lab_subnet_name").(string)
	labVirtualNetworkId := d.Get("lab_virtual_network_id").(string)
	location := azure.NormalizeLocation(d.Get("location").(string))
	notes := d.Get("notes").(string)
	password := d.Get("password").(string)
	sshKey := d.Get("ssh_key").(string)
	size := d.Get("size").(string)
	storageType := d.Get("storage_type").(string)
	username := d.Get("username").(string)

	galleryImageReferenceRaw := d.Get("gallery_image_reference").([]interface{})
	galleryImageReference := expandDevTestLabVirtualMachineGalleryImageReference(galleryImageReferenceRaw, "Linux")

	natRulesRaw := d.Get("inbound_nat_rule").(*pluginsdk.Set)
	natRules := expandDevTestLabVirtualMachineNatRules(natRulesRaw)

	if len(natRules) > 0 && !disallowPublicIPAddress {
		return fmt.Errorf("If `inbound_nat_rule` is specified then `disallow_public_ip_address` must be set to true.")
	}

	nic := dtl.NetworkInterfaceProperties{}
	if disallowPublicIPAddress {
		nic.SharedPublicIPAddressConfiguration = &dtl.SharedPublicIPAddressConfiguration{
			InboundNatRules: &natRules,
		}
	}

	authenticateViaSsh := sshKey != ""
	parameters := dtl.LabVirtualMachine{
		Location: utils.String(location),
		LabVirtualMachineProperties: &dtl.LabVirtualMachineProperties{
			AllowClaim:                 utils.Bool(allowClaim),
			IsAuthenticationWithSSHKey: utils.Bool(authenticateViaSsh),
			DisallowPublicIPAddress:    utils.Bool(disallowPublicIPAddress),
			GalleryImageReference:      galleryImageReference,
			LabSubnetName:              utils.String(labSubnetName),
			LabVirtualNetworkID:        utils.String(labVirtualNetworkId),
			NetworkInterface:           &nic,
			OsType:                     utils.String("Linux"),
			Notes:                      utils.String(notes),
			Password:                   utils.String(password),
			Size:                       utils.String(size),
			SSHKey:                     utils.String(sshKey),
			StorageType:                utils.String(storageType),
			UserName:                   utils.String(username),
		},
		Tags: tags.Expand(t),
	}

	future, err := client.CreateOrUpdate(ctx, resourceGroup, labName, name, parameters)
	if err != nil {
		return fmt.Errorf("Error creating/updating DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for creation/update of DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	read, err := client.Get(ctx, resourceGroup, labName, name, "")
	if err != nil {
		return fmt.Errorf("Error retrieving DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	if read.ID == nil {
		return fmt.Errorf("Cannot read DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q) ID", name, labName, resourceGroup)
	}

	d.SetId(*read.ID)

	return resourceArmDevTestLinuxVirtualMachineRead(d, meta)
}

func resourceArmDevTestLinuxVirtualMachineRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DevTestLabs.VirtualMachinesClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	labName := id.Path["labs"]
	name := id.Path["virtualmachines"]

	read, err := client.Get(ctx, resourceGroup, labName, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			log.Printf("[DEBUG] DevTest Linux Virtual Machine %q was not found in Lab %q / Resource Group %q - removing from state!", name, labName, resourceGroup)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error making Read request on DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	d.Set("name", read.Name)
	d.Set("lab_name", labName)
	d.Set("resource_group_name", resourceGroup)
	if location := read.Location; location != nil {
		d.Set("location", azure.NormalizeLocation(*location))
	}

	if props := read.LabVirtualMachineProperties; props != nil {
		d.Set("allow_claim", props.AllowClaim)
		d.Set("disallow_public_ip_address", props.DisallowPublicIPAddress)
		d.Set("notes", props.Notes)
		d.Set("size", props.Size)
		d.Set("storage_type", props.StorageType)
		d.Set("username", props.UserName)

		flattenedImage := flattenDevTestVirtualMachineGalleryImage(props.GalleryImageReference)
		if err := d.Set("gallery_image_reference", flattenedImage); err != nil {
			return fmt.Errorf("Error setting `gallery_image_reference`: %+v", err)
		}

		// Computed fields
		d.Set("fqdn", props.Fqdn)
		d.Set("unique_identifier", props.UniqueIdentifier)
	}

	return tags.FlattenAndSet(d, read.Tags)
}

func resourceArmDevTestLinuxVirtualMachineDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).DevTestLabs.VirtualMachinesClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	labName := id.Path["labs"]
	name := id.Path["virtualmachines"]

	read, err := client.Get(ctx, resourceGroup, labName, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(read.Response) {
			// deleted outside of TF
			log.Printf("[DEBUG] DevTest Linux Virtual Machine %q was not found in Lab %q / Resource Group %q - assuming removed!", name, labName, resourceGroup)
			return nil
		}

		return fmt.Errorf("Error retrieving DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	future, err := client.Delete(ctx, resourceGroup, labName, name)
	if err != nil {
		return fmt.Errorf("Error deleting DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for the deletion of DevTest Linux Virtual Machine %q (Lab %q / Resource Group %q): %+v", name, labName, resourceGroup, err)
	}

	return err
}
