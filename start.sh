#!/bin/bash

# API Gateway — Start Script
# Starts test backends and the gateway in one command.
# Usage: ./start.sh
# Stop:  Ctrl+C (kills all background processes)

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Track PIDs so we can clean up on exit
PIDS=()

cleanup() {
    echo ""
    echo -e "${YELLOW}Shutting down...${NC}"
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null
    echo -e "${GREEN}All processes stopped.${NC}"
    exit 0
}

trap cleanup SIGINT SIGTERM

echo -e "${CYAN}============================================${NC}"
echo -e "${CYAN}        API Gateway — Starting Up           ${NC}"
echo -e "${CYAN}============================================${NC}"
echo ""

# Start test backends
for port in 9001 9002 9003 9004; do
    echo -e "${GREEN}[backend]${NC} Starting test backend on :${port}"
    go run cmd/testbackend/main.go -port "$port" &
    PIDS+=($!)
done

# Give backends a moment to start
sleep 1

# Start the gateway
echo ""
echo -e "${GREEN}[gateway]${NC} Starting API Gateway on :8080"
echo -e "${CYAN}--------------------------------------------${NC}"
go run cmd/gateway/main.go &
PIDS+=($!)

# Wait a moment then print help
sleep 1
echo ""
echo -e "${CYAN}============================================${NC}"
echo -e "${CYAN}  Gateway is running! Try these commands:   ${NC}"
echo -e "${CYAN}============================================${NC}"
echo ""
echo -e "  ${YELLOW}Health check:${NC}"
echo "    curl http://localhost:8080/health | jq"
echo ""
echo -e "  ${YELLOW}API request (with auth):${NC}"
echo "    curl -H 'X-API-Key: key-abc123' http://localhost:8080/api/v1/hello"
echo ""
echo -e "  ${YELLOW}Round-robin test:${NC}"
echo "    for i in 1 2 3; do curl -s -H 'X-API-Key: key-abc123' http://localhost:8080/api/v1/hello | jq .port; done"
echo ""
echo -e "  ${YELLOW}Rate limit test:${NC}"
echo "    for i in \$(seq 1 15); do curl -s -o /dev/null -w '%{http_code}\n' -H 'X-API-Key: key-abc123' http://localhost:8080/api/v1/hello; done"
echo ""
echo -e "  Press ${YELLOW}Ctrl+C${NC} to stop everything."
echo ""

# Wait for all processes
wait
