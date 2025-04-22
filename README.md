# Sandboxed

A secure sandbox environment for running code in isolated Docker containers with real-time output streaming.

## Features

- Remote code execution in isolated Docker containers
- Real-time output streaming via WebSocket
- Automatic container cleanup
- Resource limits for containers

## Prerequisites

- Go 1.21+
- Docker
- Docker API version 1.43+

## Quick Start

1. Build and run the server:
```bash
go build -o server ./cmd/api
./server
```

## API

### WebSocket Endpoint: `/v1/execute`

Send Go code to execute:
```json
{
    "text": "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}"
}
```

The server will:
- Create an isolated Docker container
- Execute the provided Go code
- Stream the output in real-time
- Clean up the container after execution

## Configuration

- `-port`: Server port (default: 4000)
- `-env`: Environment (development|staging|production)
