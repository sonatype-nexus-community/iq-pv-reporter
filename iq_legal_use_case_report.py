#!/usr/bin/env python3
#
# Copyright 2019-Present Sonatype Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

"""
Sonatype Lifecycle Legal Use Case Report Generator

This script generates a report identifying all repositories/applications and their
associated Legal Use Case (Application Category), License Policy Violations triggered,
and component/license information.
"""

import argparse
import csv
import json
import re
import sys
from datetime import datetime
from typing import Any, Dict, List, Optional, Tuple

import requests
from requests.auth import HTTPBasicAuth
from tabulate import tabulate

# Disable SSL warnings when --no-verify-ssl is used
import urllib3
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)


class IqServerError(Exception):
    """Custom exception for IQ Server API errors."""
    pass


class IqClient:
    """Client for interacting with Sonatype IQ Server API."""

    ROOT_ORG_ID = "ROOT_ORGANIZATION_ID"

    def __init__(self, base_url: str, auth: HTTPBasicAuth, verify_ssl: bool = True, debug: bool = False):
        self.base_url = base_url.rstrip('/')
        self.auth = auth
        self.verify_ssl = verify_ssl
        self.debug = debug
        self.session = requests.Session()

        # Configure session with CSRF token handling (required by IQ)
        self.session.cookies.set('CLM-CSRF-TOKEN', 'api')
        self.session.headers = {'X-CSRF-TOKEN': 'api'}

    def _log_debug(self, message: str) -> None:
        """Log debug messages if debug mode is enabled."""
        if self.debug:
            print(f"[DEBUG] {datetime.now().isoformat()} - {message}")

    def _make_request(self, method: str, endpoint: str, params: Optional[Dict] = None) -> Any:
        """Make an authenticated request to IQ Server API."""
        url = f"{self.base_url}{endpoint}"
        self._log_debug(f"Making {method} request to {url} with params: {params}")

        try:
            if method.upper() == 'GET':
                response = self.session.get(
                    url,
                    auth=self.auth,
                    params=params,
                    verify=self.verify_ssl,
                    timeout=60
                )
            elif method.upper() == 'POST':
                response = self.session.post(
                    url,
                    auth=self.auth,
                    json=params,
                    verify=self.verify_ssl,
                    timeout=60
                )
            else:
                raise ValueError(f"Unsupported HTTP method: {method}")

            self._log_debug(f"Response status: {response.status_code}")

            if response.status_code == 401:
                raise IqServerError("Authentication failed. Check your credentials.")
            elif response.status_code == 403:
                raise IqServerError("Access denied. Insufficient permissions.")
            elif response.status_code == 404:
                self._log_debug(f"Resource not found: {endpoint}")
                return None
            elif response.status_code != 200:
                raise IqServerError(f"API error: {response.status_code} - {response.text}")

            return response.json()

        except requests.exceptions.ConnectionError:
            raise IqServerError(f"Could not connect to IQ Server at {self.base_url}")
        except requests.exceptions.Timeout:
            raise IqServerError(f"Request timed out connecting to IQ Server at {self.base_url}")
        except json.JSONDecodeError:
            self._log_debug(f"Response was not JSON: {response.text[:500]}")
            return None

    def get_organizations(self) -> List[Dict]:
        """Fetch all organizations."""
        self._log_debug("Fetching organizations...")
        result = self._make_request('GET', '/api/v2/organizations')
        orgs = result.get('organizations', []) if result else []
        self._log_debug(f"Found {len(orgs)} organizations")
        return orgs

    def get_applications(self) -> List[Dict]:
        """Fetch all applications with categories."""
        self._log_debug("Fetching applications with categories...")
        result = self._make_request('GET', '/api/v2/applications', params={'includeCategories': 'true'})
        apps = result.get('applications', []) if result else []
        self._log_debug(f"Found {len(apps)} applications")
        return apps

    def get_application_categories(self) -> List[Dict]:
        """Fetch all available application categories."""
        self._log_debug("Fetching application categories...")
        result = self._make_request('GET', f'/api/v2/applicationCategories/organization/{self.ROOT_ORG_ID}')
        categories = result if isinstance(result, list) else []
        self._log_debug(f"Found {len(categories)} application categories")
        return categories

    def get_policies(self) -> List[Dict]:
        """Fetch all policies."""
        self._log_debug("Fetching policies...")
        result = self._make_request('GET', '/api/v2/policies')
        policies = result if isinstance(result, list) else (result.get('policies', []) if result else [])
        self._log_debug(f"Found {len(policies)} policies")
        return policies

    def get_policy_violations(self, policy_ids: List[str], include_waived: bool = True) -> List[Dict]:
        """Fetch policy violations for given policy IDs."""
        self._log_debug(f"Fetching policy violations for {len(policy_ids)} policies...")

        all_violations = []

        # Process policies in batches to avoid URL length limits
        batch_size = 10
        for i in range(0, len(policy_ids), batch_size):
            batch = policy_ids[i:i + batch_size]

            params = {'p': batch}
            if include_waived:
                params['type'] = ['ACTIVE', 'WAIVED', 'LEGACY']
            else:
                params['type'] = ['ACTIVE']

            result = self._make_request('GET', '/api/v2/policyViolations', params=params)

            if result and 'applicationViolations' in result:
                all_violations.extend(result['applicationViolations'])

        self._log_debug(f"Found violations for {len(all_violations)} applications")
        return all_violations


class LegalUseCaseReport:
    """Generates the Legal Use Case report."""

    # Constants
    NOT_ASSIGNED = "⚠️ NOT ASSIGNED"
    NOT_ASSIGNED_PLAIN = "NOT ASSIGNED"

    def __init__(self, client: IqClient, legal_policy_prefix: str, stage_filter: Optional[str] = None):
        self.client = client
        self.legal_policy_prefix = legal_policy_prefix
        self.stage_filter = stage_filter

        # Data stores
        self.orgs_by_id: Dict[str, str] = {}
        self.categories_by_id: Dict[str, str] = {}
        self.legal_policy_ids: List[str] = []
        self.applications: List[Dict] = []
        self.violations: List[Dict] = []

        # Report data
        self.report_rows: List[Dict] = []

    def build_org_lookup(self, orgs: List[Dict]) -> None:
        """Build organization ID to name lookup."""
        self.orgs_by_id = {org['id']: org['name'] for org in orgs}

    def build_category_lookup(self, categories: List[Dict]) -> None:
        """Build category ID to name lookup."""
        self.categories_by_id = {cat['id']: cat['name'] for cat in categories}

    def find_legal_policies(self, policies: List[Dict]) -> List[str]:
        """Find policies matching the legal policy prefix."""
        matching = []
        for policy in policies:
            policy_name = policy.get('name', '')
            if policy_name.startswith(self.legal_policy_prefix):
                matching.append(policy['id'])
                self.client._log_debug(f"Found legal policy: {policy_name} (ID: {policy['id']})")

        if not matching:
            print(f"⚠️  Warning: No policies found with prefix '{self.legal_policy_prefix}'")
            print(f"   Available policies: {[p.get('name') for p in policies[:10]]}...")

        return matching

    def get_application_category(self, app: Dict) -> str:
        """Extract the Application Category for an application.

        The categories field is populated when includeCategories=true is passed.
        Categories are returned as a list of objects with 'id', 'name', 'description', etc.
        """
        categories = app.get('categories', [])
        if not categories:
            return self.NOT_ASSIGNED

        # An app can have multiple categories - join them
        category_names = []
        for cat in categories:
            # Categories come with the full object including name directly
            cat_name = cat.get('name')
            if cat_name:
                category_names.append(cat_name)

        if not category_names:
            return self.NOT_ASSIGNED

        return ", ".join(category_names)

    def extract_license_from_reason(self, reason: str) -> str:
        """Extract license ID/name from violation reason text."""
        # License reasons often look like "License (Apache-2.0) triggered..."
        # or "Component has license (GPL-3.0) that..."
        license_match = re.search(r'\(([^)]+)\)', reason)
        if license_match:
            return license_match.group(1)
        return reason[:50] if len(reason) > 50 else reason

    def build_report(self) -> None:
        """Build the complete report data."""
        print("Collecting data from IQ Server...")

        # Step 1: Fetch organizations
        print("  → Fetching organizations...")
        orgs = self.client.get_organizations()
        self.build_org_lookup(orgs)

        # Step 2: Fetch application categories
        print("  → Fetching application categories...")
        categories = self.client.get_application_categories()
        self.build_category_lookup(categories)

        # Step 3: Fetch applications
        print("  → Fetching applications...")
        self.applications = self.client.get_applications()

        # Step 4: Fetch policies and find legal policies
        print("  → Fetching policies...")
        policies = self.client.get_policies()
        self.legal_policy_ids = self.find_legal_policies(policies)

        # Step 5: Fetch policy violations if legal policies exist
        if self.legal_policy_ids:
            print(f"  → Fetching policy violations for {len(self.legal_policy_ids)} legal policies...")
            self.violations = self.client.get_policy_violations(self.legal_policy_ids)
        else:
            print("  → No legal policies found, skipping violation fetch...")

        # Step 6: Build violation lookup by application ID
        violations_by_app: Dict[str, List[Dict]] = {}
        for app_violation in self.violations:
            app = app_violation.get('application', {})
            app_id = app.get('id') or app.get('publicId')
            if app_id:
                if app_id not in violations_by_app:
                    violations_by_app[app_id] = []
                violations_by_app[app_id].append(app_violation)

        # Step 7: Build report rows
        print("  → Building report data...")

        for app in self.applications:
            app_id = app.get('id')
            app_public_id = app.get('publicId', 'N/A')
            app_name = app.get('name', app_public_id)
            org_id = app.get('organizationId')
            org_name = self.orgs_by_id.get(org_id, 'Unknown Organization')
            application_category = self.get_application_category(app)

            # Get violations for this app
            app_violations = violations_by_app.get(app_id, [])

            # Skip apps with no violations - report focuses on policy violations only
            if not app_violations or not any(
                pv.get('policyViolations') for pv in app_violations
            ):
                continue
            else:
                # Process each violation
                for app_violation in app_violations:
                    for pv in app_violation.get('policyViolations', []):
                        # Filter by stage if specified
                        if self.stage_filter:
                            stage_id = pv.get('stageId', '')
                            if stage_id.lower() != self.stage_filter.lower():
                                continue

                        policy_name = pv.get('policyName', 'Unknown Policy')
                        threat_level = pv.get('threatLevel', '')

                        # Determine violation status
                        is_waived = pv.get('isWaived', False)
                        is_legacy = pv.get('isLegacy', False)
                        if is_waived:
                            status = "WAIVED"
                        elif is_legacy:
                            status = "LEGACY"
                        else:
                            status = "ACTIVE"

                        # Get component info
                        component = pv.get('component', {})
                        component_purl = component.get('packageUrl', '')
                        component_name = component.get('displayName', component_purl)

                        # Process constraint violations to get licenses
                        constraint_violations = pv.get('constraintViolations', [])
                        for cv in constraint_violations:
                            constraint_name = cv.get('constraintName', 'Unknown Constraint')

                            # Extract licenses from reasons
                            licenses = []
                            for reason_obj in cv.get('reasons', []):
                                reason = reason_obj.get('reason', '')
                                license_info = self.extract_license_from_reason(reason)
                                if license_info and license_info not in licenses:
                                    licenses.append(license_info)

                            triggering_licenses = ", ".join(licenses) if licenses else ""

                            self.report_rows.append({
                                'org_name': org_name,
                                'app_name': app_name,
                                'app_public_id': app_public_id,
                                'application_category': application_category,
                                'policy_name': policy_name,
                                'threat_level': threat_level,
                                'constraint_name': constraint_name,
                                'triggering_licenses': triggering_licenses,
                                'component_purl': component_purl,
                                'component_name': component_name,
                                'violation_status': status,
                                'waiver_details': ''  # Could be enriched from waiver data
                            })

        print(f"✓ Report data built: {len(self.report_rows)} rows")

    def print_terminal_report(self) -> None:
        """Print formatted terminal report."""
        print("\n" + "=" * 80)
        print("SONATYPE LIFECYCLE - POLICY VIOLATION REPORT")
        print(f"Generated: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
        print("=" * 80)

        # Single table with all policy violations
        if not self.report_rows:
            print("\nNo policy violations found matching the criteria.")
            print("=" * 80 + "\n")
            return

        # Build table data
        table_data = []
        for r in self.report_rows:
            table_data.append([
                r['org_name'][:25] if len(r['org_name']) > 25 else r['org_name'],
                r['app_name'][:30] if len(r['app_name']) > 30 else r['app_name'],
                r['application_category'][:30] if len(r['application_category']) > 30 else r['application_category'],
                r['policy_name'],
                r['threat_level'],
                r['component_purl'][:45] if r['component_purl'] else '',
                r['triggering_licenses'][:30] if r['triggering_licenses'] else '',
                r['violation_status']
            ])

        print(tabulate(
            table_data,
            headers=['Organization', 'Application', 'App Category', 'Policy', 'Threat', 'Component PURL', 'License', 'Status'],
            tablefmt='simple'
        ))

        # Summary
        print("\n" + "=" * 80)
        print("SUMMARY")
        print("=" * 80)

        total_apps = len(set((r['org_name'], r['app_name']) for r in self.report_rows))

        # Count apps with/without category
        apps_with_category = set(
            (r['org_name'], r['app_name']) for r in self.report_rows
            if self.NOT_ASSIGNED_PLAIN not in r['application_category']
        )
        apps_without_category = set(
            (r['org_name'], r['app_name']) for r in self.report_rows
            if self.NOT_ASSIGNED_PLAIN in r['application_category']
        )

        total_violations = len(self.report_rows)

        print(f"Total Applications with Violations: {total_apps}")
        print(f"  With Application Category:        {len(apps_with_category)}")
        print(f"  Without Application Category:     {len(apps_without_category)}  ⚠️" if apps_without_category else f"  Without Application Category:     0")
        print(f"Total Policy Violations:            {total_violations}")
        print("=" * 80 + "\n")

    def write_csv_report(self, output_path: str) -> None:
        """Write report to CSV file."""
        fieldnames = [
            'org_name',
            'app_name',
            'app_public_id',
            'application_category',
            'policy_name',
            'threat_level',
            'constraint_name',
            'triggering_licenses',
            'component_purl',
            'component_name',
            'violation_status',
            'waiver_details'
        ]

        with open(output_path, 'w', newline='', encoding='utf-8') as csvfile:
            writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
            writer.writeheader()
            writer.writerows(self.report_rows)

        print(f"✓ CSV report written to: {output_path}")


def parse_args() -> argparse.Namespace:
    """Parse command line arguments."""
    parser = argparse.ArgumentParser(
        description='Generate Legal Use Case Report from Sonatype Lifecycle',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Basic usage with username/password
  %(prog)s -u https://iq.example.com --username admin --password admin123 -p "Legal-"

  # With user token instead of password
  %(prog)s -u https://iq.example.com --user-token YOUR_TOKEN -p "Legal-"

  # Filter to release stage only
  %(prog)s -u https://iq.example.com --username admin --password admin123 -p "Legal-" --stage release

  # Output to CSV file
  %(prog)s -u https://iq.example.com --username admin --password admin123 -p "Legal-" -o report.csv

  # All options combined
  %(prog)s -u https://iq.example.com --username admin --password admin123 -p "Legal-" --stage build -o report.csv
        """
    )

    # Connection options
    parser.add_argument('-u', '--url', default='http://localhost:8070',
                        help='IQ Server URL (default: http://localhost:8070)')

    parser.add_argument('--username', help='Username for basic authentication')
    parser.add_argument('--password', help='Password for basic authentication (required if --username provided)')
    parser.add_argument('--user-token', help='User token for authentication (alternative to username/password)')

    # Required parameters
    parser.add_argument('-p', '--legal-policy-prefix', required=True,
                        help='Prefix to identify Legal policies (e.g., "Legal-")')

    # Optional parameters
    parser.add_argument('-s', '--stage', default='build',
                        help='Stage ID to filter violations (default: build)')

    parser.add_argument('-o', '--output',
                        help='Output CSV file path (if not provided, terminal only)')

    parser.add_argument('--include-waived', action='store_true', default=True,
                        help='Include waived violations in report (default: True)')

    parser.add_argument('--no-verify-ssl', action='store_true',
                        help='Disable SSL certificate verification')

    parser.add_argument('-d', '--debug', action='store_true',
                        help='Enable debug logging')

    args = parser.parse_args()

    # Validate authentication - must have either username+password OR user-token
    if args.username and not args.password:
        parser.error("--password is required when --username is provided")
    if not args.username and not args.user_token:
        parser.error("Either --username with --password, or --user-token is required")

    return args


def main() -> int:
    """Main entry point."""
    args = parse_args()

    # Build authentication - prefer user token if provided
    if args.user_token:
        # User token authentication - use as username with empty password
        auth = HTTPBasicAuth(args.user_token, '')
    else:
        auth = HTTPBasicAuth(args.username, args.password)

    # Create client
    try:
        client = IqClient(
            base_url=args.url,
            auth=auth,
            verify_ssl=not args.no_verify_ssl,
            debug=args.debug
        )
    except IqServerError as e:
        print(f"Error: {e}", file=sys.stderr)
        return 1

    # Create and build report
    try:
        report = LegalUseCaseReport(
            client=client,
            legal_policy_prefix=args.legal_policy_prefix,
            stage_filter=args.stage
        )
        report.build_report()
    except IqServerError as e:
        print(f"Error: {e}", file=sys.stderr)
        return 1

    # Output results
    report.print_terminal_report()

    if args.output:
        report.write_csv_report(args.output)

    return 0


if __name__ == '__main__':
    sys.exit(main())
