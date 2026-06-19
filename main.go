package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/iq"
	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/output"
	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/report"
	"github.com/sonatype-nexus-community/iq-pv-reporter/internal/util"
)

var (
	// Version is set via ldflags during build.
	version = "dev"
)

func main() {
	// CLI flags
	serverURLFlag := flag.String("url", "", "IQ Server URL (or set IQ_SERVER_URL)")
	usernameFlag := flag.String("username", "", "Username for authentication (or set IQ_SERVER_USERNAME)")
	passwordFlag := flag.String("password", "", "Password for authentication (or set IQ_SERVER_PASSWORD)")
	userTokenFlag := flag.String("user-token", "", "User token (alternative to username/password)")

	policyPrefix := flag.String("p", "", "Policy prefix filter (required, e.g., \"License-\")")
	policyPrefixLong := flag.String("policy-prefix", "", "Policy prefix filter (shorthand)")
	stage := flag.String("s", "build", "Stage ID filter (default: build)")
	stageLong := flag.String("stage", "", "Stage ID filter")
	outputFile := flag.String("o", "", "Output CSV file path")
	outputFileLong := flag.String("output", "", "Output CSV file path (shorthand)")
	includeWaived := flag.Bool("include-waived", true, "Include waived violations (default: true)")
	noVerifySSL := flag.Bool("no-verify-ssl", false, "Disable SSL certificate verification")
	debug := flag.Bool("d", false, "Enable debug logging")
	debugLong := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Show version information")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Sonatype Lifecycle Policy Violation Report Generator\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  IQ_SERVER_URL        IQ Server URL\n")
		fmt.Fprintf(os.Stderr, "  IQ_SERVER_USERNAME   Username for authentication\n")
		fmt.Fprintf(os.Stderr, "  IQ_SERVER_PASSWORD   Password for authentication\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Basic usage with username/password\n")
		fmt.Fprintf(os.Stderr, "  %s --url https://iq.example.com --username admin --password admin123 -p \"License-\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Using environment variables\n")
		fmt.Fprintf(os.Stderr, "  export IQ_SERVER_URL=https://iq.example.com\n")
		fmt.Fprintf(os.Stderr, "  export IQ_SERVER_USERNAME=admin\n")
		fmt.Fprintf(os.Stderr, "  export IQ_SERVER_PASSWORD=admin123\n")
		fmt.Fprintf(os.Stderr, "  %s -p \"License-\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Filter by stage and export to CSV\n")
		fmt.Fprintf(os.Stderr, "  %s -p \"License-\" --stage release -o report.csv\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("iq-pv-reporter version %s\n", version)
		os.Exit(0)
	}

	// Merge long and short flag values
	policyPrefixValue := *policyPrefix
	if *policyPrefixLong != "" {
		policyPrefixValue = *policyPrefixLong
	}
	stageValue := *stage
	if *stageLong != "" {
		stageValue = *stageLong
	}
	outputFileValue := *outputFile
	if *outputFileLong != "" {
		outputFileValue = *outputFileLong
	}
	debugValue := *debug || *debugLong

	// Validate required arguments
	if policyPrefixValue == "" {
		fatalf("Error: -p/--policy-prefix is required")
	}

	// Get credentials from environment or CLI flags
	serverURL := getStringOrEnv(*serverURLFlag, "IQ_SERVER_URL")
	username := getStringOrEnv(*usernameFlag, "IQ_SERVER_USERNAME")
	password := getStringOrEnv(*passwordFlag, "IQ_SERVER_PASSWORD")
	userToken := *userTokenFlag

	// Handle authentication: prefer user token if provided
	authUsername := ""
	authPassword := ""

	if userToken != "" {
		// User token authentication: use token as username with empty password
		authUsername = userToken
		authPassword = ""
	} else if username != "" && password != "" {
		authUsername = username
		authPassword = password
	} else {
		fatalf("Error: Either --username and --password (or IQ_SERVER_USERNAME/IQ_SERVER_PASSWORD env vars), or --user-token is required")
	}

	if serverURL == "" {
		fatalf("Error: --url or IQ_SERVER_URL environment variable is required")
	}

	if debugValue {
		fmt.Printf("[DEBUG] Server URL: %s\n", serverURL)
		fmt.Printf("[DEBUG] Policy Prefix: %s\n", policyPrefixValue)
		fmt.Printf("[DEBUG] Stage Filter: %s\n", stageValue)
		fmt.Printf("[DEBUG] Include Waived: %v\n", *includeWaived)
		fmt.Printf("[DEBUG] Verify SSL: %v\n", !*noVerifySSL)
	}

	// Create client
	client := iq.NewClient(serverURL, authUsername, authPassword, !*noVerifySSL)

	// Validate connection
	fmt.Println("Collecting data from IQ Server...")
	fmt.Println("  → Validating connection...")
	_, err := client.ValidateConnection()
	if err != nil {
		fatalf("Connection failed: %s", err)
	}
	if debugValue {
		fmt.Println("[DEBUG] Connection validated successfully")
	}

	// Fetch organizations
	fmt.Println("  → Fetching organizations...")
	orgs, err := client.GetOrganizations()
	if err != nil {
		fatalf("Failed to fetch organizations: %s", err)
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found %d organizations\n", len(orgs))
	}

	// Fetch applications
	fmt.Println("  → Fetching applications...")
	applications, err := client.GetApplications()
	if err != nil {
		fatalf("Failed to fetch applications: %s", err)
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found %d applications\n", len(applications))
	}

	// Fetch application categories
	fmt.Println("  → Fetching application categories...")
	categories, err := client.GetApplicationCategories()
	if err != nil {
		if debugValue {
			fmt.Printf("[DEBUG] Failed to fetch categories (non-fatal): %s\n", err)
		}
		// Categories may not be available in all IQ versions, continue without them
		categories = nil
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found %d application categories\n", len(categories))
	}

	// Fetch policies
	fmt.Println("  → Fetching policies...")
	policies, err := client.ValidateConnection()
	if err != nil {
		fatalf("Failed to fetch policies: %s", err)
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found %d policies\n", len(policies.GetPolicies()))
	}

	// Build report
	reportBuilder := report.NewBuilder(policyPrefixValue, stageValue, *includeWaived)
	reportBuilder.SetOrganizations(orgs)
	reportBuilder.SetCategories(categories)

	// Find matching policies
	legalPolicyIDs := reportBuilder.FindLegalPolicies(policies.GetPolicies())
	if len(legalPolicyIDs) == 0 {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: No policies found with prefix '%s'\n", policyPrefixValue)
		var availablePolicies []string
		for i, p := range policies.GetPolicies() {
			if i >= 10 {
				break
			}
			availablePolicies = append(availablePolicies, p.GetName())
		}
		fmt.Fprintf(os.Stderr, "   Available policies: %s...\n", strings.Join(availablePolicies, ", "))
		fmt.Fprintln(os.Stderr, "\nNo matching policies found. Exiting.")
		os.Exit(1)
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found %d policies matching prefix '%s'\n", len(legalPolicyIDs), policyPrefixValue)
	}

	// Fetch policy violations
	fmt.Printf("  → Fetching policy violations for %d legal policies...\n", len(legalPolicyIDs))
	violations, err := client.GetPolicyViolations(legalPolicyIDs, *includeWaived)
	if err != nil {
		fatalf("Failed to fetch policy violations: %s", err)
	}
	if debugValue {
		fmt.Printf("[DEBUG] Found violations for %d applications\n", len(violations))
	}

	// Build final report
	fmt.Println("  → Building report data...")
	finalReport := reportBuilder.BuildReport(applications, violations)
	fmt.Printf("✓ Report data built: %d rows\n", len(finalReport.Rows))

	// Output results
	output.PrintTerminal(finalReport)

	if outputFileValue != "" {
		err = output.PrintCSV(finalReport, outputFileValue)
		if err != nil {
			fatalf("Failed to write CSV file: %s", err)
		}
		fmt.Printf("✓ CSV report written to: %s\n", outputFileValue)
	}
}

// getStringOrEnv returns the flag value if set, otherwise checks the environment variable.
func getStringOrEnv(flagValue, envVar string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envVar)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s%s%s%s\n",
		util.ColorRed, util.ColorBold,
		fmt.Sprintf(format, args...),
		util.ColorReset)
	os.Exit(1)
}
