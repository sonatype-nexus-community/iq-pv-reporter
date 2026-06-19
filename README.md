# Sonatype Lifecycle Policy Violation Report Generator

[![License](https://img.shields.io/github/license/sonatype-nexus-community/iq-pv-reporter)](LICENSE)

A Go command-line tool to generate reports from Sonatype Lifecycle identifying all Policy Violations with associated Application Category information, component details, and license data.

---

## Features

- **Policy Violation Report**: Single table view of all policy violations
- **Application Category Tracking**: Shows which Application Category is assigned to each application
- **Prominent Flagging**: Applications without an Application Category are highlighted with ⚠️
- **Policy Filtering**: Filter policies by configurable prefix (e.g., "License-", "Legal-")
- **Component & License Details**: Shows PURL, component name, and triggering license information
- **Waiver Support**: Includes waived violations with status indicators
- **Stage Filtering**: Filter violations by lifecycle stage (build, release, source, etc.)
- **Multiple Output Formats**: Terminal display and CSV export

---

## Prerequisites

- **Sonatype IQ Server**: Version 177+ recommended
- **Access**: Valid credentials (username/password or user token) with appropriate permissions

### Required Permissions

The account used to run this tool requires:
- View IQ Elements

---

## Installation

### Download Binary

Download the latest release for your platform from the [Releases](https://github.com/sonatype-nexus-community/iq-pv-reporter/releases) page.

### Build from Source

```bash
git clone https://github.com/sonatype-nexus-community/iq-pv-reporter.git
cd iq-pv-reporter
go build -o iq-pv-reporter .
```

---

## Usage

### Basic Syntax

```bash
./iq-pv-reporter --url <IQ_URL> --username <USER> --password <PASS> -p <POLICY_PREFIX>
```

### Required Arguments

| Argument | Description |
|----------|-------------|
| `-p, --policy-prefix` | Prefix to identify policies (e.g., "License-", "Legal-") |

### Authentication (one required)

| Argument | Environment Variable | Description |
|----------|---------------------|-------------|
| `--username` + `--password` | `IQ_SERVER_USERNAME` + `IQ_SERVER_PASSWORD` | Basic authentication credentials |
| `--user-token` | | User token for authentication |

### Optional Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `--url` | (required) | IQ Server URL (or set `IQ_SERVER_URL`) |
| `-s, --stage` | `build` | Stage ID to filter violations |
| `-o, --output` | (none) | Output CSV file path |
| `--include-waived` | `true` | Include waived violations |
| `--no-verify-ssl` | `false` | Disable SSL verification |
| `-d, --debug` | `false` | Enable debug logging |
| `--version` | | Show version information |

---

## Examples

### Basic Usage with Username/Password

```bash
./iq-pv-reporter \
  --url https://iq.example.com \
  --username admin \
  --password admin123 \
  -p "License-"
```

### Using Environment Variables

```bash
export IQ_SERVER_URL=https://iq.example.com
export IQ_SERVER_USERNAME=admin
export IQ_SERVER_PASSWORD=admin123

./iq-pv-reporter -p "License-"
```

### Using User Token Authentication

```bash
./iq-pv-reporter \
  --url https://iq.example.com \
  --user-token YOUR_USER_TOKEN \
  -p "License-"
```

### Filter by Source Stage

```bash
./iq-pv-reporter \
  --url https://iq.example.com \
  --username admin \
  --password admin123 \
  -p "License-" \
  --stage source
```

### Export to CSV

```bash
./iq-pv-reporter \
  --url https://iq.example.com \
  --username admin \
  --password admin123 \
  -p "License-" \
  -o policy_violation_report.csv
```

### All Options Combined

```bash
./iq-pv-reporter \
  --url https://iq.example.com \
  --username admin \
  --password admin123 \
  -p "License-" \
  --stage build \
  -o report.csv \
  --include-waived
```

### Self-Signed Certificates

```bash
./iq-pv-reporter \
  --url https://iq-internal.company.local \
  --username admin \
  --password admin123 \
  -p "License-" \
  --no-verify-ssl
```

---

## Output

### Terminal Output

The tool generates a single table with all policy violations:

```
================================================================================
SONATYPE LIFECYCLE - POLICY VIOLATION REPORT
Generated: 2026-06-18 10:01:44
================================================================================
Organization    Application              App Category         Policy              Threat  Component PURL                    License                        Status
---------------  -----------------------  ------------------  -----------------  --------  --------------------------------  -----------------------------  --------
GH Personal     Webgoat Legacy - Madpah  Restricted-Military License-None              9  pkg:maven/java2html/j2h@1.3.1      Found 'No Source License' lice  ACTIVE
GH Personal     Webgoat Legacy - Madpah  Restricted-Military License-Copyleft           8  pkg:maven/javax.mail/mail@1.4.2    'GPL-2.0-with-classpath-except  ACTIVE
Manual Webgoat  Manual Webgoat Legacy   ⚠️ NOT ASSIGNED     License-None              9  pkg:maven/java2html/j2h@1.3.1      Found 'No Source License' lice  ACTIVE

================================================================================
SUMMARY
================================================================================
Total Applications with Violations: 19
  With Application Category:        2
  Without Application Category:     17  ⚠️
Total Policy Violations:            567
================================================================================
```

### CSV Output

When `-o` is specified, a CSV file is generated with the following columns:

| Column | Description |
|--------|-------------|
| `org_name` | Organization name |
| `app_name` | Application name |
| `app_public_id` | Application public ID |
| `application_category` | Assigned category or "⚠️ NOT ASSIGNED" |
| `policy_name` | Name of violated policy |
| `threat_level` | Threat level number |
| `constraint_name` | Constraint that triggered |
| `triggering_licenses` | License ID(s) that triggered violation |
| `component_purl` | Package URL of component |
| `component_name` | Display name of component |
| `violation_status` | ACTIVE / WAIVED / LEGACY |
| `waiver_details` | Waiver information if applicable |

---

## Policy Identification

The tool identifies policies by name prefix. This is configurable per customer implementation:

- Customer A might use: `"License-"` → `License-None`, `License-Copyleft`, etc.
- Customer B might use: `"Legal-"` → `Legal-Commercial`, `Legal-Internal`, etc.
- Customer C might use: `"License Policy -"` → `License Policy - Review`, etc.

The prefix is case-sensitive and must match the beginning of the policy name exactly.

---

## Troubleshooting

### Authentication Failed

```
Error: authentication failed (HTTP 401): check credentials
```

- Verify username and password are correct
- Ensure the account has not been locked
- Try accessing IQ Server UI with same credentials

### No Policies Found

```
⚠️  Warning: No policies found with prefix 'License-'
   Available policies: ['Security-Critical', 'Security-Moderate', ...]...
```

- Check the policy prefix matches your IQ configuration
- Verify policy names in IQ UI under Policy Management
- Ensure policies are not archived/disabled

### Connection Errors

```
Error: Connection failed: IQ API error: ...
```

- Verify IQ Server URL is correct and accessible
- Check network connectivity
- For self-signed certificates, use `--no-verify-ssl`

### Empty Report / No Violations

- Verify user has permission to view applications
- Check that policies matching the prefix have violations
- Verify stage filter matches evaluations in IQ

---

## API Endpoints Used

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v2/organizations` | List all organizations |
| `GET /api/v2/applications?includeCategories=true` | List applications with categories |
| `GET /api/v2/applicationCategories/organization/{orgId}` | Get category definitions |
| `GET /api/v2/policies` | List all policies |
| `GET /api/v2/policyViolations?p={ids}` | Get violations for policies |

---

## Development

### Building

```bash
go build -o iq-pv-reporter .
```

### Running Tests

```bash
go test ./...
```

### Building for Multiple Platforms

The project uses [GoReleaser](https://goreleaser.com/) for cross-platform builds:

```bash
goreleaser release --snapshot --clean
```

---

## Contributing

We welcome contributions! Please see the [Community Handbook](https://github.com/sonatype-nexus-community/.github/blob/main/CONTRIBUTING.md) for guidelines.

---

## License

Copyright 2019-Present Sonatype Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

---

## Support

This is a community project and is **NOT SUPPORTED** by Sonatype. Please:

- **Do NOT** file Sonatype support tickets related to this tool
- **DO** file issues on this repository for community support
