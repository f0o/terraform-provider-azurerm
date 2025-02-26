package iothub

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/iothub/mgmt/2020-03-01/devices"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/locks"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/iothub/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceIotHubRoute() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceIotHubRouteCreateUpdate,
		Read:   resourceIotHubRouteRead,
		Update: resourceIotHubRouteCreateUpdate,
		Delete: resourceIotHubRouteDelete,
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
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringMatch(
					regexp.MustCompile("^[-_.a-zA-Z0-9]{1,64}$"),
					"Route Name name can only include alphanumeric characters, periods, underscores, hyphens, has a maximum length of 64 characters, and must be unique.",
				),
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"iothub_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.IoTHubName,
			},

			"source": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringInSlice([]string{
					// TODO: This string should be fetched from the Azure Go SDK, when it is updated
					// string(devices.RoutingSourceDeviceConnectionStateEvents),
					"DeviceConnectionStateEvents",
					string(devices.RoutingSourceDeviceJobLifecycleEvents),
					string(devices.RoutingSourceDeviceLifecycleEvents),
					string(devices.RoutingSourceDeviceMessages),
					string(devices.RoutingSourceInvalid),
					string(devices.RoutingSourceTwinChangeEvents),
				}, false),
			},
			"condition": {
				// The condition is a string value representing device-to-cloud message routes query expression
				// https://docs.microsoft.com/en-us/azure/iot-hub/iot-hub-devguide-query-language#device-to-cloud-message-routes-query-expressions
				Type:     pluginsdk.TypeString,
				Optional: true,
				Default:  "true",
			},
			"endpoint_names": {
				Type: pluginsdk.TypeList,
				// Currently only one endpoint is allowed. With that comment from Microsoft, we'll leave this open to enhancement when they add multiple endpoint support.
				MaxItems: 1,
				Elem: &pluginsdk.Schema{
					Type:         pluginsdk.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
				Required: true,
			},
			"enabled": {
				Type:     pluginsdk.TypeBool,
				Required: true,
			},
		},
	}
}

func resourceIotHubRouteCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	iothubName := d.Get("iothub_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	locks.ByName(iothubName, IothubResourceName)
	defer locks.UnlockByName(iothubName, IothubResourceName)

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		if utils.ResponseWasNotFound(iothub.Response) {
			return fmt.Errorf("IotHub %q (Resource Group %q) was not found", iothubName, resourceGroup)
		}

		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	routeName := d.Get("name").(string)

	resourceId := fmt.Sprintf("%s/Routes/%s", *iothub.ID, routeName)

	source := devices.RoutingSource(d.Get("source").(string))
	condition := d.Get("condition").(string)
	endpointNamesRaw := d.Get("endpoint_names").([]interface{})
	isEnabled := d.Get("enabled").(bool)

	route := devices.RouteProperties{
		Name:          &routeName,
		Source:        source,
		Condition:     &condition,
		EndpointNames: utils.ExpandStringSlice(endpointNamesRaw),
		IsEnabled:     &isEnabled,
	}

	routing := iothub.Properties.Routing

	if routing == nil {
		routing = &devices.RoutingProperties{}
	}

	if routing.Routes == nil {
		routes := make([]devices.RouteProperties, 0)
		routing.Routes = &routes
	}

	routes := make([]devices.RouteProperties, 0)

	alreadyExists := false
	for _, existingRoute := range *routing.Routes {
		if existingRoute.Name != nil {
			if strings.EqualFold(*existingRoute.Name, routeName) {
				if d.IsNewResource() {
					return tf.ImportAsExistsError("azurerm_iothub_route", resourceId)
				}
				routes = append(routes, route)
				alreadyExists = true
			} else {
				routes = append(routes, existingRoute)
			}
		}
	}

	if d.IsNewResource() {
		routes = append(routes, route)
	} else if !alreadyExists {
		return fmt.Errorf("Unable to find Route %q defined for IotHub %q (Resource Group %q)", routeName, iothubName, resourceGroup)
	}

	routing.Routes = &routes

	future, err := client.CreateOrUpdate(ctx, resourceGroup, iothubName, iothub, "")
	if err != nil {
		return fmt.Errorf("Error creating/updating IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for the completion of the creating/updating of IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	d.SetId(resourceId)

	return resourceIotHubRouteRead(d, meta)
}

func resourceIotHubRouteRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	parsedIothubRouteId, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := parsedIothubRouteId.ResourceGroup
	iothubName := parsedIothubRouteId.Path["IotHubs"]
	routeName := parsedIothubRouteId.Path["Routes"]

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	d.Set("name", routeName)
	d.Set("iothub_name", iothubName)
	d.Set("resource_group_name", resourceGroup)

	if iothub.Properties == nil || iothub.Properties.Routing == nil {
		return nil
	}

	if routes := iothub.Properties.Routing.Routes; routes != nil {
		for _, route := range *routes {
			if route.Name != nil {
				if strings.EqualFold(*route.Name, routeName) {
					d.Set("source", route.Source)
					d.Set("condition", route.Condition)
					d.Set("enabled", route.IsEnabled)
					d.Set("endpoint_names", route.EndpointNames)
				}
			}
		}
	}

	return nil
}

func resourceIotHubRouteDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).IoTHub.ResourceClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	parsedIothubRouteId, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := parsedIothubRouteId.ResourceGroup
	iothubName := parsedIothubRouteId.Path["IotHubs"]
	routeName := parsedIothubRouteId.Path["Routes"]

	locks.ByName(iothubName, IothubResourceName)
	defer locks.UnlockByName(iothubName, IothubResourceName)

	iothub, err := client.Get(ctx, resourceGroup, iothubName)
	if err != nil {
		if utils.ResponseWasNotFound(iothub.Response) {
			return fmt.Errorf("IotHub %q (Resource Group %q) was not found", iothubName, resourceGroup)
		}

		return fmt.Errorf("Error loading IotHub %q (Resource Group %q): %+v", iothubName, resourceGroup, err)
	}

	if iothub.Properties == nil || iothub.Properties.Routing == nil {
		return nil
	}
	routes := iothub.Properties.Routing.Routes

	if routes == nil {
		return nil
	}

	updatedRoutes := make([]devices.RouteProperties, 0)
	for _, route := range *routes {
		if route.Name != nil {
			if !strings.EqualFold(*route.Name, routeName) {
				updatedRoutes = append(updatedRoutes, route)
			}
		}
	}

	iothub.Properties.Routing.Routes = &updatedRoutes

	future, err := client.CreateOrUpdate(ctx, resourceGroup, iothubName, iothub, "")
	if err != nil {
		return fmt.Errorf("Error updating IotHub %q (Resource Group %q) with Route %q: %+v", iothubName, resourceGroup, routeName, err)
	}

	if err = future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for IotHub %q (Resource Group %q) to finish updating Route %q: %+v", iothubName, resourceGroup, routeName, err)
	}

	return nil
}
