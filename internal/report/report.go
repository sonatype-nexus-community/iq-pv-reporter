package report

import (
	"regexp"
	"strings"
	"time"

	sonatypeiq "github.com/sonatype-nexus-community/nexus-iq-api-client-go"
)

const (
	NotAssigned = "⚠️ NOT ASSIGNED"
)

// ReportRow represents a single row in the policy violation report.
type ReportRow struct {
	OrgName             string
	AppName             string
	AppPublicID         string
	ApplicationCategory string
	PolicyName          string
	ThreatLevel         int32
	ConstraintName      string
	TriggeringLicenses  string
	ComponentPURL       string
	ComponentName       string
	ViolationStatus     string // ACTIVE, WAIVED, LEGACY
	WaiverDetails       string
}

// Summary contains aggregated statistics for the report.
type Summary struct {
	TotalAppsWithViolations int
	AppsWithCategory        int
	AppsWithoutCategory     int
	TotalViolations         int
}

// Report holds all policy violation data.
type Report struct {
	Rows      []ReportRow
	Summary   Summary
	Generated time.Time
}

// Builder constructs a Report from IQ API data.
type Builder struct {
	legalPolicyPrefix string
	stageFilter       string
	includeWaived     bool

	// Lookup maps
	orgsByID     map[string]string
	tagsByID     map[string]string // tagId -> tagName

	// Results
	rows []ReportRow
}

// NewBuilder creates a new report builder.
func NewBuilder(legalPolicyPrefix, stageFilter string, includeWaived bool) *Builder {
	return &Builder{
		legalPolicyPrefix: legalPolicyPrefix,
		stageFilter:       stageFilter,
		includeWaived:     includeWaived,
		orgsByID:          make(map[string]string),
		tagsByID:          make(map[string]string),
		rows:              make([]ReportRow, 0),
	}
}

// SetOrganizations builds the organization ID to name lookup.
func (b *Builder) SetOrganizations(orgs []sonatypeiq.ApiOrganizationDTO) {
	for _, org := range orgs {
		b.orgsByID[org.GetId()] = org.GetName()
	}
}

// SetCategories builds the category (tag) ID to name lookup.
func (b *Builder) SetCategories(categories []sonatypeiq.ApiApplicationCategoryDTO) {
	for _, cat := range categories {
		b.tagsByID[cat.GetId()] = cat.GetName()
	}
}

// FindLegalPolicies returns policy IDs matching the legal policy prefix.
func (b *Builder) FindLegalPolicies(policies []sonatypeiq.ApiPolicyDTO) []string {
	var matching []string
	for _, policy := range policies {
		policyName := policy.GetName()
		if strings.HasPrefix(policyName, b.legalPolicyPrefix) {
			matching = append(matching, policy.GetId())
		}
	}
	return matching
}

// GetApplicationCategory extracts the Application Category for an application.
// Applications have ApplicationTags which reference TagIds; we map these to names.
func (b *Builder) GetApplicationCategory(app sonatypeiq.ApiApplicationDTO) string {
	tags := app.GetApplicationTags()
	if len(tags) == 0 {
		return NotAssigned
	}

	var tagNames []string
	for _, tag := range tags {
		tagID := tag.GetTagId()
		if tagID == "" {
			tagID = tag.GetId()
		}
		tagName := b.tagsByID[tagID]
		if tagName != "" {
			tagNames = append(tagNames, tagName)
		}
	}

	if len(tagNames) == 0 {
		return NotAssigned
	}

	return strings.Join(tagNames, ", ")
}

// licenseRegex extracts license IDs from reason text (e.g., "(Apache-2.0)").
var licenseRegex = regexp.MustCompile(`\(([^)]+)\)`)

// ExtractLicenseFromReason extracts license ID/name from violation reason text.
func ExtractLicenseFromReason(reason string) string {
	match := licenseRegex.FindStringSubmatch(reason)
	if len(match) > 1 {
		return match[1]
	}
	if len(reason) > 50 {
		return reason[:50]
	}
	return reason
}

// BuildReport constructs the report from applications and violations.
func (b *Builder) BuildReport(
	applications []sonatypeiq.ApiApplicationDTO,
	appViolations []sonatypeiq.ApiApplicationViolationDTOV2,
) Report {
	// Build violation lookup by application ID
	violationsByApp := make(map[string][]sonatypeiq.ApiApplicationViolationDTOV2)
	for _, av := range appViolations {
		app := av.GetApplication()
		appID := app.GetId()
		if appID == "" {
			appID = app.GetPublicId()
		}
		if appID != "" {
			violationsByApp[appID] = append(violationsByApp[appID], av)
		}
	}

	// Build app lookup by ID
	appByID := make(map[string]sonatypeiq.ApiApplicationDTO)
	for _, app := range applications {
		appByID[app.GetId()] = app
	}

	// Track apps with violations for summary
	appsWithViolations := make(map[string]bool)
	appsWithCategory := make(map[string]bool)
	appsWithoutCategory := make(map[string]bool)

	// Process violations
	for appID, appViolList := range violationsByApp {
		app, appFound := appByID[appID]
		if !appFound {
			// Application in violation data but not in applications list
			// Build a minimal app from the violation data
			appData := appViolList[0].GetApplication()
			id := appData.GetId()
			name := appData.GetName()
			publicId := appData.GetPublicId()
			orgId := appData.GetOrganizationId()
			app = sonatypeiq.ApiApplicationDTO{
				Id:             &id,
				Name:           &name,
				PublicId:       &publicId,
				OrganizationId: &orgId,
			}
		}

		orgName := b.orgsByID[app.GetOrganizationId()]
		if orgName == "" {
			orgName = "Unknown Organization"
		}

		appName := app.GetName()
		if appName == "" {
			appName = app.GetPublicId()
		}

		appCategory := b.GetApplicationCategory(app)

		// Track unique apps
		appKey := orgName + "|" + appName
		appsWithViolations[appKey] = true
		if strings.Contains(appCategory, "NOT ASSIGNED") {
			appsWithoutCategory[appKey] = true
		} else {
			appsWithCategory[appKey] = true
		}

		// Process each violation
		for _, av := range appViolList {
			for _, pv := range av.GetPolicyViolations() {
				// Filter by stage if specified
				if b.stageFilter != "" {
					stageID := strings.ToLower(pv.GetStageId())
					if stageID != strings.ToLower(b.stageFilter) {
						continue
					}
				}

				// Determine violation status
				status := "ACTIVE"
				if pv.GetIsWaived() {
					status = "WAIVED"
				} else if pv.GetIsLegacy() {
					status = "LEGACY"
				}

				// Get component info
				component := pv.GetComponent()
				componentPURL := component.GetPackageUrl()
				componentName := component.GetDisplayName()
				if componentName == "" {
					componentName = componentPURL
				}

				// Process constraint violations
				for _, cv := range pv.GetConstraintViolations() {
					constraintName := cv.GetConstraintName()

					// Extract licenses from reasons
					var licenses []string
					seen := make(map[string]bool)
					for _, reasonObj := range cv.GetReasons() {
						reason := reasonObj.GetReason()
						licenseInfo := ExtractLicenseFromReason(reason)
						if licenseInfo != "" && !seen[licenseInfo] {
							licenses = append(licenses, licenseInfo)
							seen[licenseInfo] = true
						}
					}

					triggeringLicenses := strings.Join(licenses, ", ")

					b.rows = append(b.rows, ReportRow{
						OrgName:             orgName,
						AppName:             appName,
						AppPublicID:         app.GetPublicId(),
						ApplicationCategory: appCategory,
						PolicyName:          pv.GetPolicyName(),
						ThreatLevel:         pv.GetThreatLevel(),
						ConstraintName:      constraintName,
						TriggeringLicenses:  triggeringLicenses,
						ComponentPURL:       componentPURL,
						ComponentName:       componentName,
						ViolationStatus:     status,
						WaiverDetails:       "",
					})
				}
			}
		}
	}

	return Report{
		Rows: b.rows,
		Summary: Summary{
			TotalAppsWithViolations: len(appsWithViolations),
			AppsWithCategory:        len(appsWithCategory),
			AppsWithoutCategory:     len(appsWithoutCategory),
			TotalViolations:         len(b.rows),
		},
		Generated: time.Now(),
	}
}
