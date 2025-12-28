package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/abhirockzz/cosmosdb-go-sdk-helper/auth"
	util "github.com/abhirockzz/cosmosdb-go-sdk-helper/common"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testOperationDBName        = "testDatabase"
	testOperationContainerName = "testContainer"
	testPartitionKey           = "/category"

	emulatorImage = "mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-EN20251223"
	// emulatorImage    = "mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:latest"
	emulatorPort = "8081"
	healthPort   = "8080"
	//emulatorEndpoint = "https://localhost:8081"
)

var (
	emulator         testcontainers.Container
	client           *azcosmos.Client
	emulatorEndpoint string
)

// emulatorTransport is a custom http.RoundTripper that intercepts requests to the Cosmos DB emulator.
// The emulator advertises its internal port (8081) during endpoint discovery, which causes the SDK
// to try connecting to localhost:8081 instead of the mapped Testcontainers port.
// This transport rewrites the destination port to the mapped port to ensure connectivity.
type emulatorTransport struct {
	transport  http.RoundTripper
	mappedPort string
}

func (t *emulatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Port() == emulatorPort {
		req.URL.Host = fmt.Sprintf("localhost:%s", t.mappedPort)
	}
	return t.transport.RoundTrip(req)
}

func TestMain(m *testing.M) {
	// Set up the CosmosDB emulator container
	ctx := context.Background()

	var err error
	emulator, err = setupCosmosDBEmulator(ctx)
	if err != nil {
		fmt.Printf("Failed to set up CosmosDB emulator: %v\n", err)
		os.Exit(1)
	}

	mappedPort, err := emulator.MappedPort(context.Background(), emulatorPort)
	if err != nil {
		fmt.Printf("Failed to mapped port: %v\n", err)
		os.Exit(1)
	}

	baseTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Wrap the base transport with our custom emulatorTransport to handle port rewriting
	rewritingTransport := &emulatorTransport{
		transport:  baseTransport,
		mappedPort: mappedPort.Port(),
	}

	options := &azcosmos.ClientOptions{ClientOptions: azcore.ClientOptions{
		Transport: &http.Client{Transport: rewritingTransport},
	}}

	emulatorEndpoint = fmt.Sprintf("https://localhost:%s", mappedPort.Port())
	fmt.Printf("Emulator endpoint: %s\n", emulatorEndpoint)

	// Set up the CosmosDB client
	client, err = auth.GetCosmosDBClient(emulatorEndpoint, true, options)

	if err != nil {
		fmt.Printf("Failed to set up CosmosDB client: %v\n", err)
		os.Exit(1)
	}

	// Set up the database and container
	err = setupDatabaseAndContainer()
	if err != nil {
		fmt.Printf("Failed to set up database and container: %v\n", err)
		os.Exit(1)
	}

	// Seed test data
	err = seedTestData()
	if err != nil {
		fmt.Printf("Failed to seed test data: %v\n", err)
		os.Exit(1)
	}

	// Run the tests
	code := m.Run()

	// Tear down the CosmosDB emulator container
	if emulator != nil {
		_ = emulator.Terminate(ctx)
	}

	os.Exit(code)
}

// TestCreateItem tests the creation of an item in Cosmos DB
func TestCreateItem(t *testing.T) {
	os.Setenv("COSMOS_DB_ENDPOINT", emulatorEndpoint)
	os.Setenv("COSMOS_DB_DATABASE_NAME", testOperationDBName)
	os.Setenv("COSMOS_DB_CONTAINER_NAME", testOperationContainerName)

	app, err := NewApp(client)
	assert.Nil(t, err)
	assert.NotNil(t, app)

	jsonData := `{"name": "Sample Item", "description": "This is a sample item", "category": "electronics"}`

	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(jsonData))
	resp := httptest.NewRecorder()
	app.HandleCreateItem(resp, req)

	assert.Equal(t, http.StatusCreated, resp.Code)

	var createdItem Item
	err = json.NewDecoder(resp.Body).Decode(&createdItem)
	assert.Nil(t, err)

	assert.NotEmpty(t, createdItem.ID, "Expected non-empty ID")
	assert.Equal(t, "Sample Item", createdItem.Name)
	assert.Equal(t, "This is a sample item", createdItem.Description)
	assert.Equal(t, "electronics", createdItem.Category)
}

func TestCreateItem_FailureScenarios(t *testing.T) {

	os.Setenv("COSMOS_DB_ENDPOINT", emulatorEndpoint)
	os.Setenv("COSMOS_DB_DATABASE_NAME", testOperationDBName)
	os.Setenv("COSMOS_DB_CONTAINER_NAME", testOperationContainerName)

	app, err := NewApp(client)
	assert.Nil(t, err)
	assert.NotNil(t, app)

	tests := []struct {
		name           string
		method         string
		requestBody    string
		expectedStatus int
		validateDB     bool
	}{
		{
			name:           "missing name",
			method:         http.MethodPost,
			requestBody:    `{"description": "This is a sample item", "category": "electronics"}`,
			expectedStatus: http.StatusBadRequest,
			validateDB:     false,
		},
		{
			name:           "missing category",
			method:         http.MethodPost,
			requestBody:    `{"name": "Sample Item", "description": "This is a sample item"}`,
			expectedStatus: http.StatusBadRequest,
			validateDB:     false,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			requestBody:    `{"name": "Sample Item", "description":}`,
			expectedStatus: http.StatusBadRequest,
			validateDB:     false,
		},
		{
			name:           "wrong HTTP method",
			method:         http.MethodGet,
			requestBody:    `{"name": "Sample Item", "description": "This is a sample item", "category": "electronics"}`,
			expectedStatus: http.StatusMethodNotAllowed,
			validateDB:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/items", strings.NewReader(tt.requestBody))
			resp := httptest.NewRecorder()
			app.HandleCreateItem(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code)

		})
	}
}

// TestGetItem tests the retrieval of an item from Cosmos DB
func TestGetItem(t *testing.T) {
	os.Setenv("COSMOS_DB_ENDPOINT", emulatorEndpoint)
	os.Setenv("COSMOS_DB_DATABASE_NAME", testOperationDBName)
	os.Setenv("COSMOS_DB_CONTAINER_NAME", testOperationContainerName)

	app, err := NewApp(client)
	assert.Nil(t, err)
	assert.NotNil(t, app)

	// Test retrieving the seeded item
	req := httptest.NewRequest(http.MethodGet, "/items/", nil)
	req.SetPathValue("id", "test-item-1")

	q := req.URL.Query()
	q.Add("category", "electronics")
	req.URL.RawQuery = q.Encode()

	resp := httptest.NewRecorder()
	app.HandleGetItem(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var retrievedItem Item

	err = json.NewDecoder(resp.Body).Decode(&retrievedItem)
	assert.Nil(t, err)
	assert.Equal(t, "test-item-1", retrievedItem.ID)
	assert.Equal(t, "Seeded Test Item", retrievedItem.Name)
	assert.Equal(t, "This item was seeded for testing", retrievedItem.Description)
	assert.Equal(t, "electronics", retrievedItem.Category)
}

func TestGetItem_FailureScenarios(t *testing.T) {
	os.Setenv("COSMOS_DB_ENDPOINT", emulatorEndpoint)
	os.Setenv("COSMOS_DB_DATABASE_NAME", testOperationDBName)
	os.Setenv("COSMOS_DB_CONTAINER_NAME", testOperationContainerName)

	app, err := NewApp(client)
	assert.Nil(t, err)
	assert.NotNil(t, app)

	tests := []struct {
		name           string
		method         string
		itemID         string
		category       string
		setupURL       func() string
		expectedStatus int
		expectedError  string
	}{
		{
			name:     "missing item ID",
			method:   http.MethodGet,
			itemID:   "",
			category: "electronics",
			setupURL: func() string {
				return "/items/?category=electronics"
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Item ID is required",
		},
		{
			name:     "missing category parameter",
			method:   http.MethodGet,
			itemID:   "test-item-1",
			category: "",
			setupURL: func() string {
				return "/items/"
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Category query parameter is required",
		},
		{
			name:     "empty category parameter",
			method:   http.MethodGet,
			itemID:   "test-item-1",
			category: "",
			setupURL: func() string {
				return "/items/?category="
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Category query parameter is required",
		},
		{
			name:     "non-existent item ID",
			method:   http.MethodGet,
			itemID:   "non-existent-item-999",
			category: "electronics",
			setupURL: func() string {
				return "/items/?category=electronics"
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "Item not found",
		},
		{
			name:     "valid item ID but wrong category",
			method:   http.MethodGet,
			itemID:   "test-item-1",
			category: "wrong-category",
			setupURL: func() string {
				return "/items/?category=wrong-category"
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "Item not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.setupURL()
			req := httptest.NewRequest(tt.method, url, nil)

			// Set path value only if itemID is not empty
			if tt.itemID != "" {
				req.SetPathValue("id", tt.itemID)
			}

			resp := httptest.NewRecorder()
			app.HandleGetItem(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code,
				"Expected status %d but got %d for test case: %s",
				tt.expectedStatus, resp.Code, tt.name)

			// For error cases, verify the response body contains expected error message
			if tt.expectedStatus != http.StatusOK && tt.expectedError != "" {
				responseBody := resp.Body.String()
				assert.Contains(t, responseBody, tt.expectedError,
					"Expected error message '%s' in response body: %s",
					tt.expectedError, responseBody)
			}

		})
	}
}

// setupCosmosDBEmulator creates a CosmosDB emulator container for testing
func setupCosmosDBEmulator(ctx context.Context) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image: emulatorImage,
		//ExposedPorts: []string{emulatorPort + ":8081"},
		ExposedPorts: []string{emulatorPort, healthPort},
		WaitingFor:   wait.ForListeningPort(healthPort),
		Env: map[string]string{
			"ENABLE_EXPLORER": "false",
			"PROTOCOL":        "https",
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return container, nil
}

// setupDatabaseAndContainer ensures the test database and container exist
func setupDatabaseAndContainer() error {
	db, err := util.CreateDatabaseIfNotExists(client, azcosmos.DatabaseProperties{
		ID: testOperationDBName,
	}, nil)

	if err != nil {
		return err
	}

	_, err = util.CreateContainerIfNotExists(db, azcosmos.ContainerProperties{
		ID: testOperationContainerName,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
			Paths: []string{testPartitionKey},
			Kind:  azcosmos.PartitionKeyKindHash,
		},
	}, nil)

	if err != nil {
		return err
	}

	return nil
}

func seedTestData() error {
	container, err := client.NewContainer(testOperationDBName, testOperationContainerName)
	if err != nil {
		return err
	}

	// Seed known test items
	testItems := []Item{
		{
			ID:          "test-item-1",
			Name:        "Seeded Test Item",
			Description: "This item was seeded for testing",
			Category:    "electronics",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "test-item-2",
			Name:        "Another Test Item",
			Description: "Another seeded item",
			Category:    "books",
			CreatedAt:   time.Now().UTC(),
		},
	}

	for _, item := range testItems {

		partitionKey := azcosmos.NewPartitionKeyString(item.Category)
		jsonData, err := json.Marshal(item)
		if err != nil {
			return err
		}

		_, err = container.UpsertItem(context.Background(), partitionKey, jsonData, nil)
		if err != nil {
			return err
		}
	}

	return nil
}
