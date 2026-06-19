package output

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/report"
)

// PrintCSV writes the report to a CSV file.
func PrintCSV(r report.Report, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"org_name",
		"app_name",
		"app_public_id",
		"application_category",
		"policy_name",
		"threat_level",
		"constraint_name",
		"triggering_licenses",
		"component_purl",
		"component_name",
		"violation_status",
		"waiver_details",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write rows
	for _, row := range r.Rows {
		record := []string{
			row.OrgName,
			row.AppName,
			row.AppPublicID,
			row.ApplicationCategory,
			row.PolicyName,
			fmt.Sprintf("%d", row.ThreatLevel),
			row.ConstraintName,
			row.TriggeringLicenses,
			row.ComponentPURL,
			row.ComponentName,
			row.ViolationStatus,
			row.WaiverDetails,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}
