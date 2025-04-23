package main

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

func (app *application) executeHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		app.logger.Printf("[ERROR] Failed to upgrade connection: %v", err)
		app.errorResponse(w, r, http.StatusInternalServerError, "Failed to upgrade connection")
		return
	}
	defer conn.Close()

	var input struct {
		Text string `json:"text"`
	}
	err = conn.ReadJSON(&input)
	if err != nil {
		app.logger.Printf("[ERROR] Failed to read JSON: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusBadRequest)))
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.43"))
	if err != nil {
		app.logger.Printf("[ERROR] Failed to create Docker client: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:        "golang:1.21",
		Cmd:          []string{"go", "run", "main.go"},
		WorkingDir:   "/app",
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
	}, &container.HostConfig{
		AutoRemove: true,
	}, nil, nil, "")
	if err != nil {
		app.logger.Printf("[ERROR] Failed to create container: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	defer func() {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	defer tw.Close()

	if err := tw.WriteHeader(&tar.Header{
		Name: "main.go",
		Mode: 0644,
		Size: int64(len(input.Text)),
	}); err != nil {
		app.logger.Printf("[ERROR] Failed to write tar header: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	if _, err := tw.Write([]byte(input.Text)); err != nil {
		app.logger.Printf("[ERROR] Failed to write file to tar: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	if err := tw.Close(); err != nil {
		app.logger.Printf("[ERROR] Failed to close tar writer: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	if err := cli.CopyToContainer(ctx, resp.ID, "/app", &buf, container.CopyToContainerOptions{}); err != nil {
		app.logger.Printf("[ERROR] Failed to copy files to container: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		app.logger.Printf("[ERROR] Failed to start container: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	fmt.Println("container started")

	// Start streaming logs
	reader, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		app.logger.Printf("[ERROR] Failed to get container logs: %v", err)
		conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
		return
	}
	defer reader.Close()

	// Create a channel to signal when the container is done
	done := make(chan struct{})
	go func() {
		statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		select {
		case err := <-errCh:
			if err != nil {
				app.logger.Printf("[ERROR] Container wait failed: %v", err)
				conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
			}
		case <-statusCh:
			app.logger.Printf("[INFO] Container completed successfully")
		case <-ctx.Done():
			app.logger.Printf("[ERROR] Execution timed out")
			conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusRequestTimeout)))
		}
		close(done)
	}()

	// Create buffers for stdout and stderr
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	// Stream logs to WebSocket
	for {
		select {
		case <-done:
			return
		default:
			_, err := stdcopy.StdCopy(stdout, stderr, reader)
			if err != nil {
				if err != io.EOF {
					app.logger.Printf("[ERROR] Failed to read logs: %v", err)
					conn.WriteMessage(websocket.CloseMessage, []byte(http.StatusText(http.StatusInternalServerError)))
				}
				return
			}

			if stdout.Len() > 0 {
				err := conn.WriteMessage(websocket.TextMessage, stdout.Bytes())
				fmt.Println("stdout", stdout.Bytes())
				if err != nil {
					app.logger.Printf("[ERROR] Failed to write to websocket: %v", err)
					return
				}
				stdout.Reset()
			}

			if stderr.Len() > 0 {
				if err := conn.WriteMessage(websocket.TextMessage, stderr.Bytes()); err != nil {
					app.logger.Printf("[ERROR] Failed to write to websocket: %v", err)
					return
				}
				stderr.Reset()
			}
		}
	}
}
