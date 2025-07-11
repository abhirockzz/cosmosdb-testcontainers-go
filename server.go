package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/google/uuid"
)

func main() {

	endpoint := os.Getenv("COSMOS_DB_ENDPOINT")

	if endpoint == "" {
		log.Fatal("missing required environment variable COSMOS_DB_ENDPOINT")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatal(err)
	}

	client, err := azcosmos.NewClient(endpoint, cred, nil)
	if err != nil {
		log.Fatal(err)
	}

	app, err := NewApp(client)
	if err != nil {
		log.Fatalf("Failed to initialize App: %v", err)
	}

	http.HandleFunc("POST /items", app.HandleCreateItem)
	http.HandleFunc("GET /items/", app.HandleGetItem)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// App represents the application with a Cosmos DB client
// It contains methods to interact with Cosmos DB.
type App struct {
	container *azcosmos.ContainerClient
}

// NewApp creates a new application instance
func NewApp(client *azcosmos.Client) (*App, error) {

	databaseName := os.Getenv("COSMOS_DB_DATABASE_NAME")
	if databaseName == "" {
		return nil, errors.New("missing required environment variable COSMOS_DB_DATABASE_NAME")
	}

	containerName := os.Getenv("COSMOS_DB_CONTAINER_NAME")
	if containerName == "" {
		return nil, errors.New("missing required environment variable COSMOS_DB_CONTAINER_NAME")
	}
	container, err := client.NewContainer(databaseName, containerName)
	if err != nil {
		return nil, err
	}

	return &App{container: container}, nil
}

// CreateItem creates a new item in Cosmos DB
func (app *App) CreateItem(ctx context.Context, item *Item) error {
	partitionKey := azcosmos.NewPartitionKeyString(item.Category)

	jsonData, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = app.container.UpsertItem(ctx, partitionKey, jsonData, nil)
	return err
}

// GetItem retrieves an item from Cosmos DB
func (app *App) GetItem(ctx context.Context, id, category string) (*Item, error) {
	partitionKey := azcosmos.NewPartitionKeyString(category)

	resp, err := app.container.ReadItem(ctx, partitionKey, id, nil)
	if err != nil {
		return nil, err
	}

	var item Item
	if err := json.Unmarshal(resp.Value, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// handleCreateItem handles the creation of a new item
// It expects a POST request with JSON body containing item details.
func (app *App) HandleCreateItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if reqBody.Name == "" || reqBody.Category == "" {
		http.Error(w, "Name and category are required", http.StatusBadRequest)
		return
	}

	item := &Item{
		ID:          uuid.New().String(),
		Name:        reqBody.Name,
		Description: reqBody.Description,
		Category:    reqBody.Category,
		CreatedAt:   time.Now().UTC(),
	}

	if err := app.CreateItem(r.Context(), item); err != nil {
		//log.Printf("Failed to create item: %v", err)
		http.Error(w, "Failed to create item", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// handleGetItem handles the retrieval of an item by ID and category.
// It expects a GET request with the item ID in the URL path and a category query parameter
func (app *App) HandleGetItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from URL path
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Item ID is required", http.StatusBadRequest)
		return
	}

	//fmt.Println("ID:", id)

	category := r.URL.Query().Get("category")
	if category == "" {
		http.Error(w, "Category query parameter is required", http.StatusBadRequest)
		return
	}

	// fmt.Println("Category:", category)

	item, err := app.GetItem(r.Context(), id, category)
	if err != nil {
		//log.Printf("Failed to get item: %v", err)
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

// Item represents a document in Cosmos DB
type Item struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"createdAt"`
}
