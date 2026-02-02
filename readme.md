# Automated API Testing Tool (Go)

A Go tool that reads test cases from a JSON file and executes API tests in sequence.

## Features

- **Sequential Execution**: Tests run in order based on the `order` field
- **Variable Extraction & Chaining**: Extract values from responses and use them in subsequent tests
- **Response Validation**: Validate expected response structure and values
- **HTTP Status Code Validation**: Check for expected HTTP status codes
- **Colored Terminal Output**: Easy-to-read pass/fail indicators
- **Results Export**: Export detailed results to JSON file
- **Configurable Timeout**: Set timeout per test case
- **No External Dependencies**: Uses only Go standard library

## Build

```bash
go build -o api_tester api_tester.go
```

## Usage

```bash
# Basic usage
./api_tester test_cases.json

# With base URL
./api_tester -base-url https://api.example.com test_cases.json

# Stop on first failure
./api_tester -base-url https://api.example.com -stop-on-failure test_cases.json

# Export results to JSON
./api_tester -output results.json test_cases.json

# Show help
./api_tester -help
```

## JSON Configuration Format

```json
{
    "test_case": [
        {
            "test_case_name": "Example Test",
            "order": 1,
            "api": "/endpoint",
            "method": "POST",
            "headers": {
                "Content-Type": "application/json",
                "Authorization": "Bearer {{token}}"
            },
            "body": {
                "key": "value"
            },
            "params": {
                "query_param": "value"
            },
            "timeout": 30,
            "expected_status_code": 200,
            "expected_response": {
                "status": "1000",
                "data": {
                    "id": 123
                }
            },
            "extract": {
                "variable_name": "data.field.path"
            }
        }
    ]
}
```

## Field Descriptions

| Field | Required | Description |
|-------|----------|-------------|
| `test_case_name` | Yes | Name of the test case |
| `order` | Yes | Execution order (ascending) |
| `api` | Yes | API endpoint path |
| `method` | Yes | HTTP method (GET, POST, PUT, DELETE, PATCH) |
| `headers` | No | Request headers |
| `body` | No | Request body (for POST/PUT/PATCH) |
| `params` | No | URL query parameters |
| `timeout` | No | Request timeout in seconds (default: 30) |
| `expected_status_code` | No | Expected HTTP status code |
| `expected_response` | No | Expected response body (partial match) |
| `extract` | No | Variables to extract from response |

## Variable Chaining

Extract values from one test and use them in subsequent tests:

```json
{
    "test_case": [
        {
            "test_case_name": "Login",
            "order": 1,
            "api": "/auth/login",
            "method": "POST",
            "body": {"username": "user", "password": "pass"},
            "extract": {
                "token": "data.access_token",
                "user_id": "data.user.id"
            }
        },
        {
            "test_case_name": "Get Profile",
            "order": 2,
            "api": "/users/{{user_id}}",
            "method": "GET",
            "headers": {
                "Authorization": "Bearer {{token}}"
            }
        }
    ]
}
```

## Output Example

```
============================================================
  Starting API Tests - 2024-01-15 10:30:00
============================================================

[1] Login - Get Auth Token
  POST https://api.example.com/auth/login
  ↳ Extracted token = abc123...
  ↳ Extracted user_id = 42
  ✓ PASSED (150ms)

[2] Get User Profile
  GET https://api.example.com/users/42/profile
  ✓ PASSED (89ms)

[3] Invalid Request Test
  POST https://api.example.com/invalid
  ✗ FAILED (45ms)
    • status: Expected '1000', got '4000'

============================================================
  Test Summary
============================================================
  Total:  3
  Passed: 2
  Failed: 1
  Pass Rate: 66.7%
  Avg Response Time: 95ms
============================================================
```

## Exit Codes

- `0`: All tests passed
- `1`: One or more tests failed or configuration error

## Cross-Platform Build

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o api_tester-linux api_tester.go

# Windows
GOOS=windows GOARCH=amd64 go build -o api_tester.exe api_tester.go

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o api_tester-mac api_tester.go

# macOS ARM (M1/M2)
GOOS=darwin GOARCH=arm64 go build -o api_tester-mac-arm api_tester.go
```

## Example Test Case 
```json
{
    "test_case": [
        {
            "test_case_name": "Login - Get Auth Token",
            "order": 1,
            "api": "/login",
            "method": "POST",
            "headers": {
                "Content-Type": "application/json"
            },
            "body": {
                "username": "name",
                "password": "password"
            },
            "expected_status_code": 200,
            "expected_response": {
                "status": "1000"
            },
            "extract": {
                "access_token": "data.accessToken",
            }
        },
        {
            "test_case_name": "Get Terms and Conditions",
            "order": 2,
            "api": "/terms-and-conditions/get",
            "method": "POST",
            "headers": {
              "Content-Type": "application/json",
              "Authorization": "Bearer {{access_token}}"
            },
            "body": {
              "id": "01981c63-80ab-74a2-b3a1-e847b764073a"
            },
            "expected_status_code": 200,
            "expected_response": {
              "status": 1000,
              "data": {
                "id": "01981c63-80ab-74a2-b3a1-e847b764073a",
                "version": 1,
                "status": "ACTIVE"
            }
        }
    }
    ]
}
```