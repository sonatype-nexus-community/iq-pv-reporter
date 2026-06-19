package iq

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	sonatypeiq "github.com/sonatype-nexus-community/nexus-iq-api-client-go"
)

// tzOffsetRe matches bare timezone offsets like +0000 or -0500 (no colon) inside JSON strings.
// IQ Server returns RFC3339-like timestamps with +HHMM but Go requires +HH:MM.
var tzOffsetRe = regexp.MustCompile(`([+-]\d{2})(\d{2})"`)

type timestampFixTransport struct {
	base http.RoundTripper
}

func (t *timestampFixTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, err
	}
	fixed := tzOffsetRe.ReplaceAll(body, []byte(`$1:$2"`))
	resp.Body = io.NopCloser(bytes.NewReader(fixed))
	resp.ContentLength = int64(len(fixed))
	return resp, nil
}

// Client wraps the IQ API client with configuration.
type Client struct {
	apiClient *sonatypeiq.APIClient
	ctx       context.Context
}

// NewClient creates a new IQ client with the given credentials.
func NewClient(serverURL, username, password string, verifySSL bool) *Client {
	cfg := sonatypeiq.NewConfiguration()
	cfg.Servers = sonatypeiq.ServerConfigurations{{URL: strings.TrimSuffix(serverURL, "/")}}

	// Configure HTTP client
	httpClient := &http.Client{
		Transport: &timestampFixTransport{
			base: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: !verifySSL,
				},
			},
		},
	}
	cfg.HTTPClient = httpClient

	apiClient := sonatypeiq.NewAPIClient(cfg)
	ctx := context.WithValue(context.Background(), sonatypeiq.ContextBasicAuth, sonatypeiq.BasicAuth{
		UserName: username,
		Password: password,
	})

	return &Client{
		apiClient: apiClient,
		ctx:       ctx,
	}
}

// ValidateConnection tests the connection and returns the policy list.
func (c *Client) ValidateConnection() (*sonatypeiq.ApiPolicyListDTO, error) {
	result, httpResp, err := c.apiClient.PoliciesAPI.GetPolicies(c.ctx).Execute()
	if err != nil {
		if httpResp != nil {
			if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
				return nil, fmt.Errorf("authentication failed (HTTP %d): check credentials", httpResp.StatusCode)
			}
			return nil, fmt.Errorf("IQ API error (HTTP %d): %w", httpResp.StatusCode, err)
		}
		return nil, fmt.Errorf("IQ API error: %w", err)
	}
	return result, nil
}

// GetOrganizations fetches all organizations.
func (c *Client) GetOrganizations() ([]sonatypeiq.ApiOrganizationDTO, error) {
	result, httpResp, err := c.apiClient.OrganizationsAPI.GetOrganizations(c.ctx).Execute()
	if err != nil {
		if httpResp != nil {
			if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
				return nil, fmt.Errorf("authentication failed (HTTP %d)", httpResp.StatusCode)
			}
			return nil, fmt.Errorf("IQ API error (HTTP %d): %w", httpResp.StatusCode, err)
		}
		return nil, fmt.Errorf("IQ API error: %w", err)
	}
	return result.GetOrganizations(), nil
}

// GetApplications fetches all applications with categories included.
func (c *Client) GetApplications() ([]sonatypeiq.ApiApplicationDTO, error) {
	result, httpResp, err := c.apiClient.ApplicationsAPI.GetApplications(c.ctx).
		IncludeCategories(true).
		Execute()
	if err != nil {
		if httpResp != nil {
			if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
				return nil, fmt.Errorf("authentication failed (HTTP %d)", httpResp.StatusCode)
			}
			return nil, fmt.Errorf("IQ API error (HTTP %d): %w", httpResp.StatusCode, err)
		}
		return nil, fmt.Errorf("IQ API error: %w", err)
	}
	return result.GetApplications(), nil
}

// GetApplicationCategories fetches all application categories (tags) used by applications.
func (c *Client) GetApplicationCategories() ([]sonatypeiq.ApiApplicationCategoryDTO, error) {
	result, httpResp, err := c.apiClient.ApplicationCategoriesAPI.
		GetTagsUsedByApplications(c.ctx).
		Execute()
	if err != nil {
		if httpResp != nil {
			if httpResp.StatusCode == 404 {
				// Categories endpoint may not be available in all IQ versions
				return []sonatypeiq.ApiApplicationCategoryDTO{}, nil
			}
			if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
				return nil, fmt.Errorf("authentication failed (HTTP %d)", httpResp.StatusCode)
			}
			return nil, fmt.Errorf("IQ API error (HTTP %d): %w", httpResp.StatusCode, err)
		}
		return nil, fmt.Errorf("IQ API error: %w", err)
	}
	return result, nil
}

// GetPolicyViolations fetches policy violations for the given policy IDs.
func (c *Client) GetPolicyViolations(policyIDs []string, includeWaived bool) ([]sonatypeiq.ApiApplicationViolationDTOV2, error) {
	if len(policyIDs) == 0 {
		return []sonatypeiq.ApiApplicationViolationDTOV2{}, nil
	}

	// Process in batches to avoid URL length limits
	const batchSize = 10
	var allViolations []sonatypeiq.ApiApplicationViolationDTOV2

	for i := 0; i < len(policyIDs); i += batchSize {
		end := i + batchSize
		if end > len(policyIDs) {
			end = len(policyIDs)
		}
		batch := policyIDs[i:end]

		req := c.apiClient.PolicyViolationDetailsAPI.GetPolicyViolations(c.ctx).P(batch)

		if includeWaived {
			req = req.Type_([]string{"ACTIVE", "WAIVED", "LEGACY"})
		} else {
			req = req.Type_([]string{"ACTIVE"})
		}

		result, httpResp, err := req.Execute()
		if err != nil {
			if httpResp != nil {
				if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
					return nil, fmt.Errorf("authentication failed (HTTP %d)", httpResp.StatusCode)
				}
				return nil, fmt.Errorf("IQ API error (HTTP %d): %w", httpResp.StatusCode, err)
			}
			return nil, fmt.Errorf("IQ API error: %w", err)
		}

		allViolations = append(allViolations, result.GetApplicationViolations()...)
	}

	return allViolations, nil
}
