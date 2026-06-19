package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/report"
	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/util"
)

const (
	divider = "================================================================================"
)

// PrintTerminal outputs the report to stdout with ANSI colors.
func PrintTerminal(r report.Report) {
	PrintTerminalToWriter(os.Stdout, r, true)
}

// PrintTerminalToWriter outputs the report to the specified writer.
func PrintTerminalToWriter(w io.Writer, r report.Report, useColors bool) {
	printHeader(w, r.Generated, useColors)

	if len(r.Rows) == 0 {
		if useColors {
			fmt.Fprintf(w, "\n%sNo policy violations found matching the criteria.%s\n", util.ColorGreen, util.ColorReset)
		} else {
			fmt.Fprintln(w, "\nNo policy violations found matching the criteria.")
		}
		fmt.Fprintln(w, divider)
		return
	}

	// Print table
	printTable(w, r.Rows, useColors)

	// Print summary
	printSummary(w, r.Summary, useColors)
}

func printHeader(w io.Writer, generated time.Time, useColors bool) {
	fmt.Fprintln(w, divider)
	if useColors {
		fmt.Fprintf(w, "%s%sSONATYPE LIFECYCLE - POLICY VIOLATION REPORT%s\n", util.ColorCyan, util.ColorBold, util.ColorReset)
	} else {
		fmt.Fprintln(w, "SONATYPE LIFECYCLE - POLICY VIOLATION REPORT")
	}
	fmt.Fprintf(w, "Generated: %s\n", generated.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w, divider)
}

func printTable(w io.Writer, rows []report.ReportRow, useColors bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(tw, "Organization\tApplication\tApp Category\tPolicy\tThreat\tComponent PURL\tLicense\tStatus")
	fmt.Fprintln(tw, "---------------\t-----------------------\t------------------\t-----------------\t--------\t--------------------------------\t-----------------------------\t--------")

	for _, row := range rows {
		orgName := truncate(row.OrgName, 25)
		appName := truncate(row.AppName, 30)
		appCategory := truncate(row.ApplicationCategory, 30)
		componentPURL := truncate(row.ComponentPURL, 45)
		triggeringLicenses := truncate(row.TriggeringLicenses, 30)

		// Highlight apps without category
		if useColors && strings.Contains(row.ApplicationCategory, "NOT ASSIGNED") {
			appCategory = util.ColorYellow + appCategory + util.ColorReset
		}

		// Color-code threat level
		threatDisplay := fmt.Sprintf("%d", row.ThreatLevel)
		if useColors {
			threatDisplay = threatColor(row.ThreatLevel) + threatDisplay + util.ColorReset
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			orgName, appName, appCategory, row.PolicyName, threatDisplay,
			componentPURL, triggeringLicenses, row.ViolationStatus)
	}

	tw.Flush()
}

func printSummary(w io.Writer, s report.Summary, useColors bool) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, divider)
	if useColors {
		fmt.Fprintf(w, "%s%sSUMMARY%s\n", util.ColorCyan, util.ColorBold, util.ColorReset)
	} else {
		fmt.Fprintln(w, "SUMMARY")
	}
	fmt.Fprintln(w, divider)

	fmt.Fprintf(w, "Total Applications with Violations: %d\n", s.TotalAppsWithViolations)
	fmt.Fprintf(w, "  With Application Category:        %d\n", s.AppsWithCategory)

	if s.AppsWithoutCategory > 0 {
		if useColors {
			fmt.Fprintf(w, "  Without Application Category:     %d  %s⚠️%s\n", s.AppsWithoutCategory, util.ColorYellow, util.ColorReset)
		} else {
			fmt.Fprintf(w, "  Without Application Category:     %d  ⚠️\n", s.AppsWithoutCategory)
		}
	} else {
		fmt.Fprintf(w, "  Without Application Category:     0\n")
	}

	fmt.Fprintf(w, "Total Policy Violations:            %d\n", s.TotalViolations)
	fmt.Fprintln(w, divider)
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func threatColor(threat int32) string {
	switch {
	case threat >= 8:
		return util.ColorRed
	case threat >= 5:
		return util.ColorMagenta
	default:
		return util.ColorWhite
	}
}
