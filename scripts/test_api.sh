#!/bin/bash

# Default port, can be overridden by the first argument
PORT=${1:-8080}
BASE_URL="http://localhost:$PORT"

# Function to check if the server is running
check_server() {
    if ! curl -s "$BASE_URL" > /dev/null; then
        echo "API server is not running on port $PORT. Please start it first."
        echo "You can run it with: ./scripts/run.sh"
        exit 1
    fi
}

# --- Test Functions ---

test_documentation() {
    echo "--- Testing Documentation Endpoint (GET /) ---"
    curl -s -X GET "$BASE_URL/" | jq .
    echo -e "\n"
}

test_chat() {
    echo "--- Testing Chat Endpoint (POST /chat) ---"
    echo "Sending message: 'Hello, API!'"
    curl -s -X POST "$BASE_URL/chat" \
         -H "Content-Type: application/json" \
         -d '{"message": "Hello, API!"}' | jq .
    echo -e "\nNote: The AI's response will appear in the CLI console, not here."
    # Wait a moment for the server to process
    sleep 3
}

test_history() {
    echo "--- Testing History Endpoint (GET /history) ---"
    curl -s -X GET "$BASE_URL/history" | jq .
    echo -e "\n"
}


# --- Main Execution ---

echo "Starting API tests on $BASE_URL..."
check_server

test_documentation
test_chat
test_history

echo "API tests completed."
