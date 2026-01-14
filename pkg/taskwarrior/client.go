package taskwarrior

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) GetTasks(filter []string) ([]Task, error) {
	args := append(filter, "export", "rc.hooks=0")
	cmd := exec.Command("task", args...)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("taskwarrior command failed: exit code %d, %s, stderr: %s",
				exitErr.ExitCode(), err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("taskwarrior command failed: %w", err)
	}

	var tasks []Task
	if err := json.Unmarshal(output, &tasks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal taskwarrior output: %w", err)
	}
	return tasks, nil
}

// ParseTask parses a single task JSON from an io.Reader
func (c *Client) ParseTask(r io.Reader) (Task, error) {
	var task Task
	if err := json.NewDecoder(r).Decode(&task); err != nil {
		return Task{}, fmt.Errorf("failed to decode task json: %w", err)
	}
	return task, nil
}

// ParseTasks parses multiple JSON objects from an io.Reader (e.g. for hooks that send multiple lines)
func (c *Client) ParseTasks(r io.Reader) ([]Task, error) {
	var tasks []Task
	decoder := json.NewDecoder(r)
	for {
		var task Task
		if err := decoder.Decode(&task); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode task json: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}
