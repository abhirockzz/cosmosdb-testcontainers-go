# Cosmos DB Testcontainers Go Example

This repository demonstrates how to use [Testcontainers-Go](https://golang.testcontainers.org/) to run integration tests for a Go application that interacts with [Azure Cosmos DB](https://azure.microsoft.com/en-us/products/cosmos-db) using the official [azcosmos Go SDK](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos). The tests use the [Cosmos DB Emulator](https://learn.microsoft.com/en-us/azure/cosmos-db/emulator-linux) running in a Docker container.

- Simple REST API to create and retrieve items from Cosmos DB
- Integration tests that automatically start and stop the Cosmos DB emulator using Testcontainers
- Uses the Cosmos DB Go SDK and Azure AD token-based authentication for the emulator
- No external HTTP router dependencies (uses Go standard library)

## Prerequisites

- [Go](https://go.dev/doc/install) 1.21 or newer
- [Docker](https://docs.docker.com/get-docker/) (required for running tests)
- [Git](https://git-scm.com/)

## Running the Application

Set the required environment variables:

```bash
export COSMOS_DB_ENDPOINT="https://your-account.documents.azure.com:443/"
export COSMOS_DB_DATABASE_NAME="your-database" # should already exist
export COSMOS_DB_CONTAINER_NAME="your-container" # should already exist
export PORT="8080"  # Optional, defaults to 8080
```

Then run the server:

```bash
go run server.go
```

## API endpoints

- `POST /items`  
  Create a new item.  
  
  Example:

  ```bash
  curl -X POST http://localhost:8080/items \
    -H "Content-Type: application/json" \
    -d '{"name": "Sample Item", "description": "This is a sample item", "category": "electronics"}'
  ```

- `GET /items/{id}?category={category}`  
  Retrieve an item by ID and category.  
  
  Example:

  ```bash
  curl "http://localhost:8080/items/{id}?category=electronics"
  ```

## Running the tests

The integration tests use Testcontainers to spin up a Cosmos DB emulator in Docker. Make sure Docker is running.

```bash
# Pull the emulator image (optional, will be pulled automatically if not present)
docker pull mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview

# Run all tests
go test -v ./...
```

The test suite will:

- Start the Cosmos DB emulator in a container
- Set up the database and container
- Seed test data
- Run all test cases
- Clean up the container after tests

## References

- [Azure Cosmos DB Go SDK](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos)
- [Testcontainers-Go Documentation](https://golang.testcontainers.org/)
- [Azure Cosmos DB Emulator (Linux)](https://learn.microsoft.com/en-us/azure/cosmos-db/emulator-linux)
- [Cosmos DB Emulator vs. Cloud Service Differences](https://learn.microsoft.com/en-us/azure/cosmos-db/emulator?context=%2Fazure%2Fcosmos-db%2Fnosql%2Fcontext%2Fcontext#differences-between-the-emulator-and-cloud-service)
