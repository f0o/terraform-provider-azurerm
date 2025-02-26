package automation

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/preview/automation/mgmt/2018-06-30-preview/automation"
	"github.com/Azure/go-autorest/autorest/date"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	azvalidate "github.com/hashicorp/terraform-provider-azurerm/helpers/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/automation/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/set"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/suppress"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceAutomationSchedule() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceAutomationScheduleCreateUpdate,
		Read:   resourceAutomationScheduleRead,
		Update: resourceAutomationScheduleCreateUpdate,
		Delete: resourceAutomationScheduleDelete,

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
				ValidateFunc: validate.ScheduleName(),
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"automation_account_name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validate.AutomationAccount(),
			},

			"frequency": {
				Type:             pluginsdk.TypeString,
				Required:         true,
				DiffSuppressFunc: suppress.CaseDifference,
				ValidateFunc: validation.StringInSlice([]string{
					string(automation.Day),
					string(automation.Hour),
					string(automation.Month),
					string(automation.OneTime),
					string(automation.Week),
				}, true),
			},

			// ignored when frequency is `OneTime`
			"interval": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				Computed:     true, // defaults to 1 if frequency is not OneTime
				ValidateFunc: validation.IntBetween(1, 100),
			},

			"start_time": {
				Type:             pluginsdk.TypeString,
				Optional:         true,
				Computed:         true,
				DiffSuppressFunc: suppress.RFC3339Time,
				ValidateFunc:     validation.IsRFC3339Time,
				// defaults to now + 7 minutes in create function if not set
			},

			"expiry_time": {
				Type:             pluginsdk.TypeString,
				Optional:         true,
				Computed:         true, // same as start time when OneTime, ridiculous value when recurring: "9999-12-31T15:59:00-08:00"
				DiffSuppressFunc: suppress.CaseDifference,
				ValidateFunc:     validation.IsRFC3339Time,
			},

			"description": {
				Type:     pluginsdk.TypeString,
				Optional: true,
			},

			"timezone": {
				Type:         pluginsdk.TypeString,
				Optional:     true,
				Default:      "UTC",
				ValidateFunc: azvalidate.AzureTimeZoneString(),
			},

			"week_days": {
				Type:     pluginsdk.TypeSet,
				Optional: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeString,
					ValidateFunc: validation.StringInSlice([]string{
						string(automation.Monday),
						string(automation.Tuesday),
						string(automation.Wednesday),
						string(automation.Thursday),
						string(automation.Friday),
						string(automation.Saturday),
						string(automation.Sunday),
					}, true),
				},
				Set:           set.HashStringIgnoreCase,
				ConflictsWith: []string{"month_days", "monthly_occurrence"},
			},

			"month_days": {
				Type:     pluginsdk.TypeSet,
				Optional: true,
				Elem: &pluginsdk.Schema{
					Type: pluginsdk.TypeInt,
					ValidateFunc: validation.All(
						validation.IntBetween(-1, 31),
						validation.IntNotInSlice([]int{0}),
					),
				},
				Set:           set.HashInt,
				ConflictsWith: []string{"week_days", "monthly_occurrence"},
			},

			"monthly_occurrence": {
				Type:     pluginsdk.TypeList,
				Optional: true,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"day": {
							Type:             pluginsdk.TypeString,
							Required:         true,
							DiffSuppressFunc: suppress.CaseDifference,
							ValidateFunc: validation.StringInSlice([]string{
								string(automation.Monday),
								string(automation.Tuesday),
								string(automation.Wednesday),
								string(automation.Thursday),
								string(automation.Friday),
								string(automation.Saturday),
								string(automation.Sunday),
							}, true),
						},
						"occurrence": {
							Type:     pluginsdk.TypeInt,
							Required: true,
							ValidateFunc: validation.All(
								validation.IntBetween(-1, 5),
								validation.IntNotInSlice([]int{0}),
							),
						},
					},
				},
				ConflictsWith: []string{"week_days", "month_days"},
			},
		},

		CustomizeDiff: pluginsdk.CustomizeDiffShim(func(ctx context.Context, diff *pluginsdk.ResourceDiff, v interface{}) error {
			frequency := strings.ToLower(diff.Get("frequency").(string))
			interval, _ := diff.GetOk("interval")
			if frequency == "onetime" && interval.(int) > 0 {
				return fmt.Errorf("`interval` cannot be set when frequency is `OneTime`")
			}

			_, hasWeekDays := diff.GetOk("week_days")
			if hasWeekDays && frequency != "week" {
				return fmt.Errorf("`week_days` can only be set when frequency is `Week`")
			}

			_, hasMonthDays := diff.GetOk("month_days")
			if hasMonthDays && frequency != "month" {
				return fmt.Errorf("`month_days` can only be set when frequency is `Month`")
			}

			_, hasMonthlyOccurrences := diff.GetOk("monthly_occurrence")
			if hasMonthlyOccurrences && frequency != "month" {
				return fmt.Errorf("`monthly_occurrence` can only be set when frequency is `Month`")
			}

			return nil
		}),
	}
}

func resourceAutomationScheduleCreateUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Automation.ScheduleClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for AzureRM Automation Schedule creation.")

	name := d.Get("name").(string)
	resGroup := d.Get("resource_group_name").(string)
	accountName := d.Get("automation_account_name").(string)

	if d.IsNewResource() {
		existing, err := client.Get(ctx, resGroup, accountName, name)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing Automation Schedule %q (Account %q / Resource Group %q): %s", name, accountName, resGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_automation_schedule", *existing.ID)
		}
	}

	frequency := d.Get("frequency").(string)
	timeZone := d.Get("timezone").(string)
	description := d.Get("description").(string)

	parameters := automation.ScheduleCreateOrUpdateParameters{
		Name: &name,
		ScheduleCreateOrUpdateProperties: &automation.ScheduleCreateOrUpdateProperties{
			Description: &description,
			Frequency:   automation.ScheduleFrequency(frequency),
			TimeZone:    &timeZone,
		},
	}
	properties := parameters.ScheduleCreateOrUpdateProperties

	// start time can default to now + 7 (5 could be invalid by the time the API is called)
	if v, ok := d.GetOk("start_time"); ok {
		t, _ := time.Parse(time.RFC3339, v.(string)) // should be validated by the schema
		duration := time.Duration(5) * time.Minute
		if time.Until(t) < duration {
			return fmt.Errorf("start_time is %q and should be at least %q in the future", t, duration)
		}
		properties.StartTime = &date.Time{Time: t}
	} else {
		properties.StartTime = &date.Time{Time: time.Now().Add(time.Duration(7) * time.Minute)}
	}

	if v, ok := d.GetOk("expiry_time"); ok {
		t, _ := time.Parse(time.RFC3339, v.(string)) // should be validated by the schema
		properties.ExpiryTime = &date.Time{Time: t}
	}

	// only pay attention to interval if frequency is not OneTime, and default it to 1 if not set
	if properties.Frequency != automation.OneTime {
		if v, ok := d.GetOk("interval"); ok {
			properties.Interval = utils.Int32(int32(v.(int)))
		} else {
			properties.Interval = 1
		}
	}

	// only pay attention to the advanced schedule fields if frequency is either Week or Month
	if properties.Frequency == automation.Week || properties.Frequency == automation.Month {
		properties.AdvancedSchedule = expandArmAutomationScheduleAdvanced(d, d.Id() != "")
	}

	if _, err := client.CreateOrUpdate(ctx, resGroup, accountName, name, parameters); err != nil {
		return err
	}

	read, err := client.Get(ctx, resGroup, accountName, name)
	if err != nil {
		return err
	}

	if read.ID == nil {
		return fmt.Errorf("Cannot read Automation Schedule '%s' (resource group %s) ID", name, resGroup)
	}

	d.SetId(*read.ID)

	return resourceAutomationScheduleRead(d, meta)
}

func resourceAutomationScheduleRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Automation.ScheduleClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	name := id.Path["schedules"]
	resGroup := id.ResourceGroup
	accountName := id.Path["automationAccounts"]

	resp, err := client.Get(ctx, resGroup, accountName, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error making Read request on AzureRM Automation Schedule '%s': %+v", name, err)
	}

	d.Set("name", resp.Name)
	d.Set("resource_group_name", resGroup)
	d.Set("automation_account_name", accountName)
	d.Set("frequency", string(resp.Frequency))

	if v := resp.StartTime; v != nil {
		d.Set("start_time", v.Format(time.RFC3339))
	}
	if v := resp.ExpiryTime; v != nil {
		d.Set("expiry_time", v.Format(time.RFC3339))
	}
	if v := resp.Interval; v != nil {
		d.Set("interval", v)
	}
	if v := resp.Description; v != nil {
		d.Set("description", v)
	}
	if v := resp.TimeZone; v != nil {
		d.Set("timezone", v)
	}

	if v := resp.AdvancedSchedule; v != nil {
		if err := d.Set("week_days", flattenArmAutomationScheduleAdvancedWeekDays(v)); err != nil {
			return fmt.Errorf("Error setting `week_days`: %+v", err)
		}
		if err := d.Set("month_days", flattenArmAutomationScheduleAdvancedMonthDays(v)); err != nil {
			return fmt.Errorf("Error setting `month_days`: %+v", err)
		}
		if err := d.Set("monthly_occurrence", flattenArmAutomationScheduleAdvancedMonthlyOccurrences(v)); err != nil {
			return fmt.Errorf("Error setting `monthly_occurrence`: %+v", err)
		}
	}
	return nil
}

func resourceAutomationScheduleDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).Automation.ScheduleClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	name := id.Path["schedules"]
	resGroup := id.ResourceGroup
	accountName := id.Path["automationAccounts"]

	resp, err := client.Delete(ctx, resGroup, accountName, name)
	if err != nil {
		if !utils.ResponseWasNotFound(resp) {
			return fmt.Errorf("Error issuing AzureRM delete request for Automation Schedule '%s': %+v", name, err)
		}
	}

	return nil
}

func expandArmAutomationScheduleAdvanced(d *pluginsdk.ResourceData, isUpdate bool) *automation.AdvancedSchedule {
	expandedAdvancedSchedule := automation.AdvancedSchedule{}

	// If frequency is set to `Month` the `week_days` array cannot be set (even empty), otherwise the API returns an error.
	// During update it can be set and it will not return an error. Workaround for the APIs behaviour
	if v, ok := d.GetOk("week_days"); ok {
		weekDays := v.(*pluginsdk.Set).List()
		expandedWeekDays := make([]string, len(weekDays))
		for i := range weekDays {
			expandedWeekDays[i] = weekDays[i].(string)
		}
		expandedAdvancedSchedule.WeekDays = &expandedWeekDays
	} else if isUpdate {
		expandedAdvancedSchedule.WeekDays = &[]string{}
	}

	// Same as above with `week_days`
	if v, ok := d.GetOk("month_days"); ok {
		monthDays := v.(*pluginsdk.Set).List()
		expandedMonthDays := make([]int32, len(monthDays))
		for i := range monthDays {
			expandedMonthDays[i] = int32(monthDays[i].(int))
		}
		expandedAdvancedSchedule.MonthDays = &expandedMonthDays
	} else if isUpdate {
		expandedAdvancedSchedule.MonthDays = &[]int32{}
	}

	monthlyOccurrences := d.Get("monthly_occurrence").([]interface{})
	expandedMonthlyOccurrences := make([]automation.AdvancedScheduleMonthlyOccurrence, len(monthlyOccurrences))
	for i := range monthlyOccurrences {
		m := monthlyOccurrences[i].(map[string]interface{})
		occurrence := int32(m["occurrence"].(int))

		expandedMonthlyOccurrences[i] = automation.AdvancedScheduleMonthlyOccurrence{
			Occurrence: &occurrence,
			Day:        automation.ScheduleDay(m["day"].(string)),
		}
	}
	expandedAdvancedSchedule.MonthlyOccurrences = &expandedMonthlyOccurrences

	return &expandedAdvancedSchedule
}

func flattenArmAutomationScheduleAdvancedWeekDays(s *automation.AdvancedSchedule) *pluginsdk.Set {
	flattenedWeekDays := pluginsdk.NewSet(set.HashStringIgnoreCase, []interface{}{})
	if weekDays := s.WeekDays; weekDays != nil {
		for _, v := range *weekDays {
			flattenedWeekDays.Add(v)
		}
	}
	return flattenedWeekDays
}

func flattenArmAutomationScheduleAdvancedMonthDays(s *automation.AdvancedSchedule) *pluginsdk.Set {
	flattenedMonthDays := pluginsdk.NewSet(set.HashInt, []interface{}{})
	if monthDays := s.MonthDays; monthDays != nil {
		for _, v := range *monthDays {
			flattenedMonthDays.Add(int(v))
		}
	}
	return flattenedMonthDays
}

func flattenArmAutomationScheduleAdvancedMonthlyOccurrences(s *automation.AdvancedSchedule) []map[string]interface{} {
	flattenedMonthlyOccurrences := make([]map[string]interface{}, 0)
	if monthlyOccurrences := s.MonthlyOccurrences; monthlyOccurrences != nil {
		for _, v := range *monthlyOccurrences {
			f := make(map[string]interface{})
			f["day"] = v.Day
			f["occurrence"] = int(*v.Occurrence)
			flattenedMonthlyOccurrences = append(flattenedMonthlyOccurrences, f)
		}
	}
	return flattenedMonthlyOccurrences
}
