package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3" // Used for calendar.CalendarEventsScope
	"google.golang.org/api/option"
)

const (
	// ClientSecretsFile is the path to your downloaded Google API credentials.json file.
	// This file contains your client_id, client_secret, and redirect_uris.
	// Place this file in your project root, or adjust the path accordingly.
	ClientSecretsFile = "credentials.json"

	// TokenFile is the path where the user's obtained OAuth token (access_token + refresh_token)
	// will be stored. It's recommended to place this in a user's config directory (e.g., ~/.config/taskwarrior-agenda/token.json)
	// For simplicity in this example, it's relative to the execution directory.
	TokenFile = "token.json"

	// LocalhostAuthPort is the port that the local web server will listen on
	// to capture the OAuth redirect. Choose a free port.
	LocalhostAuthPort = "6789"

	xdgAppName = "taska"
)

// GetConfig creates an oauth2.Config from the client secrets file and specified scopes.
func GetConfig(scopes []string) (*oauth2.Config, error) {
	xdgConfigBase, err := GetXdgHome()
	if err != nil {
		return nil, err
	}

	clientSecretsFile := filepath.Join(xdgConfigBase, ClientSecretsFile)
	b, err := os.ReadFile(clientSecretsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file %s: %w", clientSecretsFile, err)
	}

	config, err := google.ConfigFromJSON(b, scopes...)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %w", err)
	}

	parsedURL, parseErr := url.Parse(config.RedirectURL)
	if parseErr != nil {
		log.Printf("Warning: Could not parse RedirectURL '%s': %v. Using it as is.", config.RedirectURL, parseErr)
		// Fallback for unparsable URLs, though this should ideally not happen
	} else if parsedURL.Host == "localhost" || parsedURL.Hostname() == "127.0.0.1" {
		// If it's a localhost URL, ensure it has the correct port
		if parsedURL.Port() == "" { // If port is missing
			parsedURL.Host = fmt.Sprintf("%s:%s", parsedURL.Hostname(), LocalhostAuthPort)
			config.RedirectURL = parsedURL.String()
			// log.Printf("Corrected localhost RedirectURL to: %s", config.RedirectURL)
		} else if parsedURL.Port() != LocalhostAuthPort {
			log.Printf("Warning: Mismatch in localhost redirect port. credentials.json has '%s', code expects '%s'. Using credentials.json's port.", parsedURL.Port(), LocalhostAuthPort)
			// It's crucial here that the Google Cloud Console redirect URI matches the one used by net.Listen.
			// The safest bet is to *always* force it to the LocalhostAuthPort we define.
			parsedURL.Host = fmt.Sprintf("%s:%s", parsedURL.Hostname(), LocalhostAuthPort)
			config.RedirectURL = parsedURL.String()
			log.Printf("Forcing localhost RedirectURL to match LocalhostAuthPort: %s", config.RedirectURL)
		}
	} else if config.RedirectURL == "urn:ietf:wg:oauth:2.0:oob" {
		// If it's the OOB (out-of-band) URI, force it to our preferred localhost redirect.
		config.RedirectURL = fmt.Sprintf("http://localhost:%s/oauth2callback", LocalhostAuthPort)
		log.Printf("Overriding 'urn:ietf:wg:oauth:2.0:oob' RedirectURL to: %s", config.RedirectURL)
	} else {
		// If it's not localhost and not OOB, log a warning if it's not what we expect
		log.Printf("Warning: Configured RedirectURL in credentials.json is not a localhost callback or OOB: %s. Ensure this is correct for your setup.", config.RedirectURL)
	}

	return config, nil
}

// GetClient retrieves an authenticated *http.Client.
// It tries to load an existing token, refreshes it if expired, or
// initiates a new web-based authorization flow if no token exists.
func GetClient(ctx context.Context, scopes []string) (*http.Client, error) {
	config, err := GetConfig(scopes)
	if err != nil {
		return nil, err
	}

	xdgConfigBase, err := GetXdgHome()
	if err != nil {
		return nil, err
	}

	tokenFile := filepath.Join(xdgConfigBase, TokenFile)
	tok, err := tokenFromFile(tokenFile)
	if err != nil {
		// No existing token, perform the full OAuth flow
		log.Printf("No existing token found at %s. Initiating web authorization flow...", tokenFile)
		tok, err = getTokenFromWeb(config)
		if err != nil {
			return nil, fmt.Errorf("failed to get token from web: %w", err)
		}
		saveToken(tokenFile, tok) // Save the newly obtained token
	}

	// config.Client creates an HTTP client that automatically handles token refreshing.
	// If the AccessToken is expired and a RefreshToken is available, it will use the
	// RefreshToken to get a new AccessToken.
	client := config.Client(ctx, tok)

	// It's good practice to ensure the token in TokenFile is always the latest valid one,
	// especially after an automatic refresh by config.Client().
	// We get the token from the TokenSource created by config.Client
	// and re-save it if it has changed (e.g., AccessToken was refreshed).
	// Note: It's rare but possible for the RefreshToken itself to change,
	// so always saving the whole token is safest.
	go func() {
		currentTok, err := config.TokenSource(ctx, tok).Token()
		if err != nil {
			log.Printf("Warning: Could not get current token from source for re-saving: %v", err)
			return
		}
		// Compare access tokens for simplicity, if they differ, save the new one.
		// A more robust check might compare entire token structs, but access token change
		// is the most common indication of a refresh.
		if currentTok.AccessToken != tok.AccessToken || currentTok.RefreshToken != tok.RefreshToken {
			log.Println("Token was refreshed or updated. Saving new token to file.")
			saveToken(tokenFile, currentTok)
		}
	}()

	return client, nil
}

// getTokenFromWeb initiates the OAuth 2.0 authorization code flow via a local web server.
// It opens a browser window for the user to grant permission and captures the redirect.
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	// Create a channel to receive the authorization code
	codeCh := make(chan string)
	errCh := make(chan error)

	// Start a local HTTP server to capture the redirect
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", LocalhostAuthPort))
	if err != nil {
		return nil, fmt.Errorf("failed to start listener on port %s: %w", LocalhostAuthPort, err)
	}
	defer listener.Close() // Ensure listener is closed

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				http.Error(w, "Authorization code not found", http.StatusBadRequest)
				errCh <- fmt.Errorf("authorization code not found in redirect URL")
				return
			}
			fmt.Fprintf(w, "Authentication successful! You can close this window.")
			codeCh <- code // Send the code to the channel
		}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	go func() {
		log.Printf("Local server listening on %s for OAuth2 redirect...", config.RedirectURL)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	// Construct the authorization URL
	// AccessTypeOffline is crucial to ensure a refresh token is returned.
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	fmt.Printf("Please open the following URL in your browser to authorize TaskwarriorAgenda:\n%s\n", authURL)

	// Attempt to open the URL in the default browser (platform-dependent)
	// You might need a more robust cross-platform solution for this.
	// For simple cases, `go run` often opens it automatically if you have
	// a "Desktop App" client type configured to OOB or localhost redirect.
	log.Println("Waiting for authorization code...")

	select {
	case authCode := <-codeCh:
		// Exchange the authorization code for tokens
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := config.Exchange(ctx, authCode)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve token from Google: %w", err)
		}
		// Shut down the local server after successful exchange
		server.Shutdown(ctx)
		return tok, nil
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute): // Timeout for the user to authorize
		server.Shutdown(context.Background()) // Attempt to shut down server on timeout
		return nil, fmt.Errorf("authorization timed out. Please try again")
	}
}

// tokenFromFile reads an oauth2.Token from a JSON file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	if err != nil {
		return nil, fmt.Errorf("failed to decode token from file %s: %w", file, err)
	}
	return tok, nil
}

// saveToken saves an oauth2.Token to a JSON file.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving authentication token to: %s\n", path)
	// Create the directory if it doesn't exist
	dir := os.Args[0] // Default to current executable directory if no specific path is given
	if len(path) > 0 && path[len(path)-1] != os.PathSeparator {
		dir = path[:len(path)-len("token.json")] // Get directory from path
	}
	if dir == "" {
		dir = "." // Current directory
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Warning: Could not create token directory %s: %v", dir, err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600) // 0600: read/write for owner only
	if err != nil {
		log.Fatalf("Unable to cache OAuth token to %s: %v", path, err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// GetCalendarService creates an authenticated Google Calendar service.
// This is the function your main application logic (e.g., `sync.go`) will call.
func GetCalendarService(ctx context.Context) (*calendar.Service, error) {
	// Define the necessary scopes for your application.
	// calendar.CalendarEventsScope: Allows viewing and editing events on all calendars.
	// This is typically sufficient for a sync tool.
	scopes := []string{
		calendar.CalendarEventsScope,
		calendar.CalendarReadonlyScope,
	}

	client, err := GetClient(ctx, scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticated client for Calendar API: %w", err)
	}

	// Create the Calendar service using the authenticated HTTP client.
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Google Calendar service: %w", err)
	}
	return srv, nil
}

func GetXdgHome() (string, error) {
	xdgHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(xdgHome, ".config", xdgAppName), nil
}
