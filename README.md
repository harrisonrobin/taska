# taska

**taska** is a robust Taskwarrior hook that synchronizes your tasks with Google Calendar. It operates transparently in the background, keeping your calendar up-to-date as you add, modify, or delete tasks in Taskwarrior.

## Features

*   **Real-time Synchronization:** Acts as an `on-add` and `on-modify` hook to instantly sync changes.
*   **Smart Event Management:**
    *   Adds new tasks to Google Calendar.
    *   Deletes calendar events if a task is marked as `deleted` or moves to a `waiting` status.
    *   Updates existing events when task details (start time, completion status, etc.) change.
*   **Authentication:** Simple OAuth2 authentication flow to securely connect to your Google account.
*   **Persistent Configuration:** Set your preferred calendar once and it works everywhere.

## Prerequisites

*   [Taskwarrior](https://taskwarrior.org/) installed.
*   Go installed (version 1.23 or later).
*   A Google Cloud project with the Google Calendar API enabled.
*   Credentials file (`credentials.json`) for your Google Cloud project.

## Installation

```bash
go install github.com/harrisonrobin/taska@latest
```

## Configuration

1.  **Setup Configuration Directory:**
    Ensure you have a configuration directory at `~/.config/taska`.

    ```bash
    mkdir -p ~/.config/taska
    ```

2.  **Google Cloud Credentials:**
    *   Download `credentials.json` from Google Cloud Console.
    *   Place it in `~/.config/taska/`.

3.  **Authenticate:**
    Run the binary with the `--auth` flag to generate your token.

    ```bash
    taska --auth
    ```
    Follow the instructions to authorize the application.

4.  **Set Your Preferred Calendar (Optional):**
    By default, `taska` syncs to a calendar named "Tasks". You can change this persistently:

    ```bash
    taska --set-calendar "Work"
    ```

5.  **Taskwarrior Hook Setup:**
    Link the binary to your Taskwarrior hooks directory.

    ```bash
    # Assuming taska is in your $GOPATH/bin or $PATH
    ln -s $(which taska) ~/.task/hooks/on-add.taska
    ln -s $(which taska) ~/.task/hooks/on-modify.taska
    ```

    *Note: Ensure the hook is executable.*

## Usage

Once installed as a hook, **taska** works automatically using the calendar you configured (or "Tasks" by default).

*   **Add a Task:** `task add "Meeting with Client" due:tomorrow` -> Creates a calendar event.
*   **Complete a Task:** `task 1 done` -> Updates/Removes event depending on logic.
*   **Delete/Wait:** `task 1 delete` or `task 1 wait:1w` -> Removes the event from the calendar.

### Manual Sync / Debugging

You can manually pipe Taskwarrior's JSON output to `taska` to test behavior:

```bash
task export | taska
```

### Options

*   `--set-calendar "Calendar Name"`: Sets the default calendar for future runs.
*   `--calendar "Calendar Name"`: Overrides the configured calendar for a *single* run.
*   `--auth`: Trigger authentication flow.

## Contributing

Contributions are welcome! Please submit a pull request.
