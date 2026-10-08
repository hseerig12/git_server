package main

import (
	"bytes"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const BaseStoragePath = "/Users/yash/Work/git-server-dev/repos"

func main() {
	go startWebServer()

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			authorizedKeysBytes, err := os.ReadFile("authorized_keys")
			if err != nil {
				return nil, err
			}

			for len(authorizedKeysBytes) > 0 {
				var pub ssh.PublicKey
				pub, _, _, authorizedKeysBytes, err = ssh.ParseAuthorizedKey(authorizedKeysBytes)
				if err != nil {
					break
				}
				if bytes.Equal(pubKey.Marshal(), pub.Marshal()) {
					log.Printf("Access Granted: User '%s'", conn.User())
					return nil, nil
				}
			}
			return nil, os.ErrPermission
		},
	}

	privateBytes, err := os.ReadFile("server_ed25519")
	if err != nil {
		log.Fatalf("Failed to load private key: %v", err)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatalf("Failed to parse private key: %v", err)
	}
	config.AddHostKey(private)

	listener, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Println("Secure Git SSH Server listening on 0.0.0.0:2222...")

	for {
		nConn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(nConn, config)
	}
}

// --- SSH LOGIC ---
func handleConnection(nConn net.Conn, config *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "Only session channels allowed")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go handleSession(channel, requests)
	}
}

func handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	for req := range requests {
		if req.Type == "exec" {
			var payload struct{ Command string }
			ssh.Unmarshal(req.Payload, &payload)
			cmdLine := payload.Command

			if !strings.HasPrefix(cmdLine, "git-receive-pack") && !strings.HasPrefix(cmdLine, "git-upload-pack") {
				if req.WantReply {
					req.Reply(false, nil)
				}
				return
			}
			if req.WantReply {
				req.Reply(true, nil)
			}

			parts := strings.SplitN(cmdLine, " ", 2)
			if len(parts) != 2 {
				return
			}

			requestedPath := strings.Trim(parts[1], "'\" /")

			repoName := filepath.Base(filepath.Clean(requestedPath))

			// Security check against bad paths
			if repoName == "." || repoName == "/" || strings.HasPrefix(repoName, "..") {
				log.Printf("Security Violation: Invalid path requested")
				return
			}

			// Now it joins directly: /.../repos + repo.git
			fullDiskPath := filepath.Join(BaseStoragePath, repoName)

			if _, err := os.Stat(fullDiskPath); os.IsNotExist(err) {
				os.MkdirAll(fullDiskPath, 0755)
				exec.Command("git", "init", "--bare", fullDiskPath).Run()
				log.Printf("Auto-provisioned: %s", fullDiskPath)
			}

			cmd := exec.Command(parts[0], fullDiskPath)
			cmd.Stdin = channel
			cmd.Stdout = channel
			cmd.Stderr = channel.Stderr()

			err := cmd.Run()
			exitStatus := struct{ Status uint32 }{0}
			if err != nil {
				exitStatus.Status = 1
			}
			channel.SendRequest("exit-status", false, ssh.Marshal(exitStatus))
			return
		}
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// --- WEB SERVER LOGIC ---
type PageData struct {
	Title       string
	RepoName    string
	Repos       []string
	Files       []string
	FileContent string
}

func startWebServer() {
	// 1. Setup Static File Server for CSS
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 2. Setup Dynamic Routes
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Ignore favicon requests to prevent log noise
		if r.URL.Path == "/favicon.ico" {
			return
		}

		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

		if r.URL.Path == "/" || len(pathParts) == 0 || pathParts[0] == "" {
			var repos []string
			entries, _ := os.ReadDir(BaseStoragePath)
			for _, e := range entries {
				if e.IsDir() {
					repos = append(repos, e.Name())
				}
			}
			renderTemplate(w, PageData{Title: "Repositories", Repos: repos})
			return
		}

		repoName := pathParts[0]
		repoPath := filepath.Join(BaseStoragePath, repoName)

		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			http.Error(w, "Repository not found", http.StatusNotFound)
			return
		}

		if len(pathParts) == 1 {
			cmd := exec.Command("git", "-C", repoPath, "ls-tree", "-r", "--name-only", "HEAD")
			out, err := cmd.Output()

			var files []string
			if err == nil && len(strings.TrimSpace(string(out))) > 0 {
				files = strings.Split(strings.TrimSpace(string(out)), "\n")
			}

			renderTemplate(w, PageData{Title: repoName, RepoName: repoName, Files: files})
			return
		}

		filePath := strings.Join(pathParts[1:], "/")
		cmd := exec.Command("git", "-C", repoPath, "show", "HEAD:"+filePath)
		out, err := cmd.Output()
		if err != nil {
			http.Error(w, "File not found in git history", http.StatusNotFound)
			return
		}

		renderTemplate(w, PageData{
			Title:       filePath,
			RepoName:    repoName,
			FileContent: string(out),
		})
	})

	log.Println("Web UI Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func renderTemplate(w http.ResponseWriter, data PageData) {
	t, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Printf("Template parsing error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, data)
}
