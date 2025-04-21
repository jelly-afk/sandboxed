package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func (app *application) executeHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Text string `json:"text"`
	}

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		app.errorResponse(w, r, http.StatusBadRequest, "Invalid request payload")
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.43"))
	if err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, "Failed to create Docker client")
		return
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:        "golang:1.21",
		Cmd:          []string{"go", "run", "main.go"},
		WorkingDir:   "/app",
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
	}, &container.HostConfig{
		AutoRemove: true,
	}, nil, nil, "")
	if err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to create container: %v", err))
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
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to write tar header: %v", err))
		return
	}

	if _, err := tw.Write([]byte(input.Text)); err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to write file to tar: %v", err))
		return
	}

	if err := tw.Close(); err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to close tar writer: %v", err))
		return
	}

	if err := cli.CopyToContainer(ctx, resp.ID, "/app", &buf, container.CopyToContainerOptions{}); err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to copy files to container: %v", err))
		return
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to start container: %v", err))
		return
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Error waiting for container: %v", err))
			return
		}
	case <-statusCh:
	case <-ctx.Done():
		app.errorResponse(w, r, http.StatusRequestTimeout, "Execution timed out")
		return
	}

	logs, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to get container logs: %v", err))
		return
	}
	defer logs.Close()

	stdCopyBuf := new(bytes.Buffer)
	_, err = stdcopy.StdCopy(stdCopyBuf, stdCopyBuf, logs)
	if err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, fmt.Sprintf("Failed to read container logs: %v", err))
		return
	}

	response := map[string]string{
		"status": "success",
		"output": stdCopyBuf.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		app.errorResponse(w, r, http.StatusInternalServerError, "Failed to encode response")
		return
	}
}
