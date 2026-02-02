package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Terminal color codes
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[91m"
	ColorGreen  = "\033[92m"
	ColorYellow = "\033[93m"
	ColorBlue   = "\033[94m"
	ColorCyan   = "\033[96m"
	ColorBold   = "\033[1m"
)

// Default values
const (
	DefaultTimeout     = 30 // seconds
	DefaultFileMode    = 0644
	SeparatorLength    = 60
	MinPassRateGreen   = 100.0
	MinPassRateYellow  = 80.0
)

// TestCase represents a single test case from JSON
type TestCase struct {
	TestCaseName       string                 `json:"test_case_name"`
	Order              int                    `json:"order"`
	API                string                 `json:"api"`
	Method             string                 `json:"method"`
	Headers            map[string]string      `json:"headers"`
	Body               map[string]interface{} `json:"body"`
	Params             map[string]string      `json:"params"`
	Timeout            int                    `json:"timeout"`
	ExpectedStatusCode int                    `json:"expected_status_code"`
	ExpectedResponse   map[string]interface{} `json:"expected_response"`
	Extract            map[string]string      `json:"extract"`
}

// Config represents the JSON configuration file structure
type Config struct {
	TestCases []TestCase `json:"test_case"`
}

// TestResult stores the result of a test execution
type TestResult struct {
	TestCaseName       string      `json:"test_case_name"`
	Order              int         `json:"order"`
	Method             string      `json:"method"`
	URL                string      `json:"url"`
	Status             string      `json:"status"`
	Errors             []string    `json:"errors"`
	ResponseTimeMs     float64     `json:"response_time_ms"`
	ResponseStatusCode int         `json:"response_status_code"`
	ResponseBody       interface{} `json:"response_body"`
}

// TestReport represents the final test report
type TestReport struct {
	Timestamp  string         `json:"timestamp"`
	ConfigFile string         `json:"config_file"`
	BaseURL    string         `json:"base_url"`
	Summary    map[string]int `json:"summary"`
	Results    []TestResult   `json:"results"`
}

// APITester handles the test execution
type APITester struct {
	ConfigPath    string
	BaseURL       string
	TestCases     []TestCase
	Results       []TestResult
	Variables     map[string]interface{}
	HTTPClient    *http.Client
	StopOnFailure bool
}

// NewAPITester creates a new APITester instance
func NewAPITester(configPath, baseURL string, stopOnFailure bool) *APITester {
	return &APITester{
		ConfigPath:    configPath,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		Variables:     make(map[string]interface{}),
		HTTPClient:    &http.Client{},
		StopOnFailure: stopOnFailure,
	}
}

// LoadConfig loads and validates the JSON configuration file
func (t *APITester) LoadConfig() error {
	file, err := os.ReadFile(t.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	t.TestCases = config.TestCases

	// Sort by order
	sort.Slice(t.TestCases, func(i, j int) bool {
		return t.TestCases[i].Order < t.TestCases[j].Order
	})

	fmt.Printf("%s✓ Loaded %d test cases%s\n", ColorGreen, len(t.TestCases), ColorReset)
	return nil
}

// replaceVariables replaces {{variable}} placeholders with stored values
func (t *APITester) replaceVariables(input string) string {
	result := input
	for varName, varValue := range t.Variables {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", varValue))
	}
	return result
}

// replaceInMap replaces variables in all values of a map
func (t *APITester) replaceInMap(input map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range input {
		result[key] = t.replaceVariables(value)
	}
	return result
}

// replaceInInterface recursively replaces variables in any data structure
func (t *APITester) replaceInInterface(input interface{}) interface{} {
	switch value := input.(type) {
	case string:
		return t.replaceVariables(value)
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range value {
			result[key] = t.replaceInInterface(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(value))
		for i, val := range value {
			result[i] = t.replaceInInterface(val)
		}
		return result
	default:
		return input
	}
}

// getNestedValue extracts a nested value using dot notation (e.g., "data.user.id")
func getNestedValue(data interface{}, path string) interface{} {
	keys := strings.Split(path, ".")
	current := data

	for _, key := range keys {
		switch v := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = v[key]
			if !ok {
				return nil
			}
		case []interface{}:
			index, err := strconv.Atoi(key)
			if err != nil || index >= len(v) {
				return nil
			}
			current = v[index]
		default:
			return nil
		}
	}
	return current
}

// extractVariables extracts variables from response based on 'extract' field
func (t *APITester) extractVariables(testCase TestCase, responseData interface{}) {
	for varName, path := range testCase.Extract {
		value := getNestedValue(responseData, path)
		if value != nil {
			t.Variables[varName] = value
			fmt.Printf("  %s↳ Extracted %s = %v%s\n", ColorCyan, varName, value, ColorReset)
		}
	}
}

// ValidateResponse recursively validates actual response against expected values
func (t *APITester) ValidateResponse(expected, actual interface{}, path string) []string {
	var errors []string

	switch expectedValue := expected.(type) {
	case map[string]interface{}:
		actualMap, ok := actual.(map[string]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: Expected object, got %T", path, actual)}
		}

		for key, expVal := range expectedValue {
			currentPath := key
			if path != "" {
				currentPath = path + "." + key
			}

			actualVal, exists := actualMap[key]
			if !exists {
				errors = append(errors, fmt.Sprintf("%s: Key not found in response", currentPath))
			} else {
				errors = append(errors, t.ValidateResponse(expVal, actualVal, currentPath)...)
			}
		}

	case []interface{}:
		actualArray, ok := actual.([]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: Expected array, got %T", path, actual)}
		}

		for i, expItem := range expectedValue {
			currentPath := fmt.Sprintf("%s[%d]", path, i)
			if i >= len(actualArray) {
				errors = append(errors, fmt.Sprintf("%s: Index out of range", currentPath))
			} else {
				errors = append(errors, t.ValidateResponse(expItem, actualArray[i], currentPath)...)
			}
		}

	default:
		if !compareValues(expected, actual) {
			errors = append(errors, fmt.Sprintf("%s: Expected '%v', got '%v'", path, expected, actual))
		}
	}

	return errors
}

// compareValues compares two values, handling type differences
func compareValues(expected, actual interface{}) bool {
	return fmt.Sprintf("%v", expected) == fmt.Sprintf("%v", actual)
}

// buildURL constructs the full URL for the API request
func (t *APITester) buildURL(testCase TestCase) string {
	api := t.replaceVariables(testCase.API)
	if t.BaseURL != "" {
		return t.BaseURL + api
	}
	return api
}

// setTimeout sets the HTTP client timeout for the request
func (t *APITester) setTimeout(testCase TestCase) {
	timeout := testCase.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	t.HTTPClient.Timeout = time.Duration(timeout) * time.Second
}

// prepareRequestBody prepares the JSON body for POST/PUT/PATCH requests
func (t *APITester) prepareRequestBody(testCase TestCase, method string) (io.Reader, error) {
	if testCase.Body == nil {
		return nil, nil
	}

	if method != "POST" && method != "PUT" && method != "PATCH" {
		return nil, nil
	}

	bodyWithVars := t.replaceInInterface(testCase.Body)
	bodyBytes, err := json.Marshal(bodyWithVars)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body: %w", err)
	}

	return bytes.NewReader(bodyBytes), nil
}

// createHTTPRequest creates and configures an HTTP request
func (t *APITester) createHTTPRequest(method, url string, body io.Reader, testCase TestCase) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	headers := t.replaceInMap(testCase.Headers)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Set query parameters
	if testCase.Params != nil {
		params := t.replaceInMap(testCase.Params)
		query := req.URL.Query()
		for key, value := range params {
			query.Add(key, value)
		}
		req.URL.RawQuery = query.Encode()
	}

	return req, nil
}

// executeRequest performs the HTTP request and measures response time
func (t *APITester) executeRequest(req *http.Request) (*http.Response, float64, error) {
	startTime := time.Now()
	resp, err := t.HTTPClient.Do(req)
	elapsed := time.Since(startTime)
	return resp, float64(elapsed.Milliseconds()), err
}

// parseResponseBody reads and parses the response body
func parseResponseBody(resp *http.Response) (interface{}, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var responseData interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		// If not JSON, return as string
		return string(body), nil
	}

	return responseData, nil
}

// validateTestResult validates response against expected values
func (t *APITester) validateTestResult(testCase TestCase, result *TestResult, responseData interface{}) {
	// Validate HTTP status code
	if testCase.ExpectedStatusCode != 0 && result.ResponseStatusCode != testCase.ExpectedStatusCode {
		result.Errors = append(result.Errors,
			fmt.Sprintf("HTTP Status: Expected %d, got %d",
				testCase.ExpectedStatusCode, result.ResponseStatusCode))
	}

	// Validate response body
	if testCase.ExpectedResponse != nil {
		validationErrors := t.ValidateResponse(testCase.ExpectedResponse, responseData, "")
		result.Errors = append(result.Errors, validationErrors...)
	}
}

// printTestResult prints the test result with appropriate formatting
func printTestResult(result TestResult) {
	if len(result.Errors) > 0 {
		fmt.Printf("  %s✗ FAILED (%.0fms)%s\n", ColorRed, result.ResponseTimeMs, ColorReset)
		for _, err := range result.Errors {
			fmt.Printf("    %s• %s%s\n", ColorRed, err, ColorReset)
		}
	} else {
		fmt.Printf("  %s✓ PASSED (%.0fms)%s\n", ColorGreen, result.ResponseTimeMs, ColorReset)
	}
}

// RunTest executes a single test case
func (t *APITester) RunTest(testCase TestCase) TestResult {
	result := TestResult{
		TestCaseName: testCase.TestCaseName,
		Order:        testCase.Order,
		Method:       strings.ToUpper(testCase.Method),
		Status:       "PENDING",
		Errors:       []string{},
	}

	// Build URL and configure timeout
	result.URL = t.buildURL(testCase)
	t.setTimeout(testCase)

	// Print test header
	fmt.Printf("\n%s[%d] %s%s\n", ColorBold, testCase.Order, testCase.TestCaseName, ColorReset)
	fmt.Printf("  %s%s %s%s\n", ColorBlue, result.Method, result.URL, ColorReset)

	// Prepare request body
	bodyReader, err := t.prepareRequestBody(testCase, result.Method)
	if err != nil {
		result.Status = "FAILED"
		result.Errors = append(result.Errors, err.Error())
		fmt.Printf("  %s✗ FAILED - Body preparation error%s\n", ColorRed, ColorReset)
		return result
	}

	// Create HTTP request
	req, err := t.createHTTPRequest(result.Method, result.URL, bodyReader, testCase)
	if err != nil {
		result.Status = "FAILED"
		result.Errors = append(result.Errors, err.Error())
		fmt.Printf("  %s✗ FAILED - Request creation error%s\n", ColorRed, ColorReset)
		return result
	}

	// Execute request
	resp, responseTime, err := t.executeRequest(req)
	result.ResponseTimeMs = responseTime
	if err != nil {
		result.Status = "FAILED"
		result.Errors = append(result.Errors, fmt.Sprintf("Request failed: %v", err))
		fmt.Printf("  %s✗ FAILED - %v%s\n", ColorRed, err, ColorReset)
		return result
	}
	defer resp.Body.Close()

	result.ResponseStatusCode = resp.StatusCode

	// Parse response body
	responseData, err := parseResponseBody(resp)
	if err != nil {
		result.Status = "FAILED"
		result.Errors = append(result.Errors, err.Error())
		fmt.Printf("  %s✗ FAILED - Response read error%s\n", ColorRed, ColorReset)
		return result
	}
	result.ResponseBody = responseData

	// Extract variables from response
	t.extractVariables(testCase, responseData)

	// Validate response against expectations
	t.validateTestResult(testCase, &result, responseData)

	// Set final status and print result
	if len(result.Errors) > 0 {
		result.Status = "FAILED"
	} else {
		result.Status = "PASSED"
	}
	printTestResult(result)

	return result
}

// printTestHeader prints the test execution header
func printTestHeader() {
	separator := strings.Repeat("=", SeparatorLength)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n%s%s%s\n", ColorBold, separator, ColorReset)
	fmt.Printf("%s  Starting API Tests - %s%s\n", ColorBold, timestamp, ColorReset)
	fmt.Printf("%s%s%s\n", ColorBold, separator, ColorReset)
}

// RunAllTests executes all test cases in order
func (t *APITester) RunAllTests() {
	printTestHeader()
	t.Results = []TestResult{}

	for _, testCase := range t.TestCases {
		result := t.RunTest(testCase)
		t.Results = append(t.Results, result)

		if t.StopOnFailure && result.Status == "FAILED" {
			fmt.Printf("\n%s⚠ Stopping execution due to failure%s\n", ColorYellow, ColorReset)
			break
		}
	}
}

// calculateSummary computes test statistics from results
func (t *APITester) calculateSummary() (total, passed, failed int) {
	total = len(t.Results)
	for _, result := range t.Results {
		if result.Status == "PASSED" {
			passed++
		} else {
			failed++
		}
	}
	return
}

// calculateAverageResponseTime computes average response time from results
func (t *APITester) calculateAverageResponseTime() float64 {
	var totalTime float64
	var count int

	for _, result := range t.Results {
		if result.ResponseTimeMs > 0 {
			totalTime += result.ResponseTimeMs
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return totalTime / float64(count)
}

// getPassRateColor returns the appropriate color based on pass rate
func getPassRateColor(passRate float64) string {
	if passRate >= MinPassRateGreen {
		return ColorGreen
	}
	if passRate >= MinPassRateYellow {
		return ColorYellow
	}
	return ColorRed
}

// PrintSummary prints a summary of all test results
func (t *APITester) PrintSummary() bool {
	total, passed, failed := t.calculateSummary()

	fmt.Printf("\n%s%s%s\n", ColorBold, strings.Repeat("=", SeparatorLength), ColorReset)
	fmt.Printf("%s  Test Summary%s\n", ColorBold, ColorReset)
	fmt.Printf("%s%s%s\n", ColorBold, strings.Repeat("=", SeparatorLength), ColorReset)
	fmt.Printf("  Total:  %d\n", total)
	fmt.Printf("  %sPassed: %d%s\n", ColorGreen, passed, ColorReset)
	fmt.Printf("  %sFailed: %d%s\n", ColorRed, failed, ColorReset)

	if total > 0 {
		passRate := float64(passed) / float64(total) * 100
		color := getPassRateColor(passRate)
		fmt.Printf("  %sPass Rate: %.1f%%%s\n", color, passRate, ColorReset)
	}

	avgResponseTime := t.calculateAverageResponseTime()
	if avgResponseTime > 0 {
		fmt.Printf("  Avg Response Time: %.0fms\n", avgResponseTime)
	}

	fmt.Printf("%s\n", strings.Repeat("=", SeparatorLength))

	return passed == total
}

// ExportResults exports test results to a JSON file
func (t *APITester) ExportResults(outputPath string) error {
	total, passed, failed := t.calculateSummary()

	report := TestReport{
		Timestamp:  time.Now().Format(time.RFC3339),
		ConfigFile: t.ConfigPath,
		BaseURL:    t.BaseURL,
		Summary: map[string]int{
			"total":  total,
			"passed": passed,
			"failed": failed,
		},
		Results: t.Results,
	}

	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(outputPath, jsonData, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write results file: %w", err)
	}

	fmt.Printf("%s✓ Results exported to: %s%s\n", ColorGreen, outputPath, ColorReset)
	return nil
}

// printUsage prints the command-line usage information
func printUsage() {
	fmt.Fprintf(os.Stderr, "Automated API Testing Tool\n\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [options] <config.json>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s test_cases.json\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -base-url https://api.example.com test_cases.json\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -base-url https://api.example.com -stop-on-failure test_cases.json\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -output results.json test_cases.json\n", os.Args[0])
}

// parseCommandLineArgs parses and validates command-line arguments
func parseCommandLineArgs() (baseURL, output, configPath string, stopOnFailure bool) {
	baseURLFlag := flag.String("base-url", "", "Base URL for all API endpoints")
	stopOnFailureFlag := flag.Bool("stop-on-failure", false, "Stop execution after first failure")
	outputFlag := flag.String("output", "", "Export results to JSON file")
	help := flag.Bool("help", false, "Show help message")

	flag.Usage = printUsage
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	// Get config file path
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "%sError: Config file path required%s\n\n", ColorRed, ColorReset)
		flag.Usage()
		os.Exit(1)
	}

	return *baseURLFlag, *outputFlag, args[0], *stopOnFailureFlag
}

func main() {
	baseURL, output, configPath, stopOnFailure := parseCommandLineArgs()

	// Create and initialize tester
	tester := NewAPITester(configPath, baseURL, stopOnFailure)

	if err := tester.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "%sError: %v%s\n", ColorRed, err, ColorReset)
		os.Exit(1)
	}

	// Run tests and print summary
	tester.RunAllTests()
	allPassed := tester.PrintSummary()

	// Export results if requested
	if output != "" {
		if err := tester.ExportResults(output); err != nil {
			fmt.Fprintf(os.Stderr, "%sError: %v%s\n", ColorRed, err, ColorReset)
		}
	}

	// Exit with error code if tests failed
	if !allPassed {
		os.Exit(1)
	}
}
