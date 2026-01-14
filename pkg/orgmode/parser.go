package orgmode

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/harrisonrobin/taska/pkg/model"
)

// parseFile parses an Org-mode file and returns a slice of tasks.
func parseFile(filePath string) ([]model.Task, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file, filePath)
}

// ParseFiles parses multiple Org-mode files and returns a slice of tasks.
func ParseFiles(filePaths []string) ([]model.Task, error) {
	var allTasks []model.Task
	for _, filePath := range filePaths {
		tasks, err := parseFile(filePath)
		if err != nil {
			return nil, err
		}
		allTasks = append(allTasks, tasks...)
	}
	return allTasks, nil
}

// Parse parses an Org-mode reader and returns a slice of tasks.
func Parse(r io.Reader, source string) ([]model.Task, error) {
	fmt.Printf("parsing file: %s\n", source)
	scanner := bufio.NewScanner(r)
	var tasks []model.Task
	var currentTask *model.Task

	todoRegex := regexp.MustCompile(`^\* TODO\s*(?:\[#([A-Z])\])?\s*(.*?)(?:\s+(:(\w+(:\w+)*):))?\s*$`)
	doneRegex := regexp.MustCompile(`^\* DONE\s*(?:\[#([A-Z])\])?\s*(.*?)(?:\s+(:(\w+(:\w+)*):))?\s*$`)
	deadlineRegex := regexp.MustCompile(`DEADLINE:\s+<(\d{4}-\d{2}-\d{2}\s+[A-Za-z]{3}\s+\d{2}:\d{2})>`)
	idRegex := regexp.MustCompile(`:ID:\s+([a-fA-F0-9-]+)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		isTodoPrefix := strings.HasPrefix(line, "* TODO")
		isDonePrefix := strings.HasPrefix(line, "* DONE")
		isEndPrefix := strings.HasPrefix(line, ":END:")
		if isTodoPrefix || isDonePrefix {
			var status string
			var matches []string
			if isTodoPrefix {
				matches = todoRegex.FindStringSubmatch(line)
				status = "pending"
			} else {
				matches = doneRegex.FindStringSubmatch(line)
				status = "completed"
			}
			currentTask = &model.Task{Source: source, Status: status}
			if len(matches) > 0 {
				currentTask.Priority = matches[1]
				currentTask.Description = strings.TrimSpace(matches[2])
				if len(matches) > 3 {
					tags := strings.Trim(matches[3], ":")
					currentTask.Tags = strings.Split(tags, ":")
				}
			}
		} else if currentTask != nil {
			if matches := deadlineRegex.FindStringSubmatch(line); len(matches) > 0 {
				deadline, err := time.ParseInLocation("2006-01-02 Mon 15:04", matches[1], time.Local)
				if err == nil {
					currentTask.Deadline = deadline
				}
			} else if matches := idRegex.FindStringSubmatch(line); len(matches) > 0 {
				currentTask.ID = matches[1]
			}
		}

		if isEndPrefix {
			if currentTask != nil && currentTask.Description != "" && currentTask.ID != "" && !currentTask.Deadline.IsZero() {
				tasks = append(tasks, *currentTask)
			}
		}

	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

// FilterTasks filters a slice of tasks by a given filter string.
// Currently, it only supports filtering by a single tag.
func FilterTasks(tasks []model.Task, filter string) []model.Task {
	var filteredTasks []model.Task
	for _, task := range tasks {
		for _, tag := range task.Tags {
			if tag == filter {
				filteredTasks = append(filteredTasks, task)
				break
			}
		}
	}
	return filteredTasks
}
