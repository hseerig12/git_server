
---

## 🛠️ Setup & Installation

### Prerequisites
* **Go** (1.18+ recommended)
* **Git** installed on the host machine.

### Step 1: Create the Sandbox
Clone this repository or create the base directory structure:
```bash
mkdir -p git-server/{templates,static,repos}
cd git-server
```
*Note: Update the `BaseStoragePath` constant at the top of `main.go` to match the absolute path to your `repos/` directory.*

### Step 2: Generate the Server's Host Key (Identity)
SSH is a two-way street. The server needs its own cryptographic identity to prove to connecting clients that it is not a Man-in-the-Middle attacker. 
Run this inside your project root to generate a password-less Ed25519 key strictly for the server:
```bash
ssh-keygen -t ed25519 -f server_ed25519 -q -N ""
```

### Step 3: Setup User Authentication
To allow your personal machine to push code, you need to add your personal public key to the server's VIP list.
1. Create the authorized keys file: `touch authorized_keys`
2. Copy your local machine's public key (usually found at `~/.ssh/id_ed25519.pub` or `~/.ssh/id_rsa.pub`).
3. Paste the contents directly into the `authorized_keys` file.

### Step 4: Install Dependencies
Initialize your Go module and download the required cryptographic packages:
```bash
go mod init gitserver
go get golang.org/x/crypto/ssh
```

---

## 🚀 Running the Server

Start the dual-headed server (SSH + HTTP):
```bash
go run main.go
```
*You should see logs indicating that the SSH server is listening on `0.0.0.0:2222` and the Web UI is running on `http://localhost:8080`.*

---

## 💻 Usage (Client Side)

Because the server supports auto-provisioning, you do not need to create repositories on the server manually. Just push!

### Pushing a New Project
In a completely separate terminal window, initialize a new project and push it to your server:
```bash
mkdir my-project
cd my-project
git init
echo "# Hello World" > README.md
git add .
git commit -m "Initial commit"

# Add the remote (format: ssh://localhost:2222/<namespace>/<repo-name>.git)
git remote add origin ssh://localhost:2222/yash/my-project.git

# Push to the server
git push origin main
```
*Note: On your first connection, your terminal will ask if you trust the host fingerprint. Type `yes` to accept the `server_ed25519` key we generated in Step 2.*

### Cloning a Project
```bash
git clone ssh://localhost:2222/yash/my-project.git
```

### Viewing the Code
Open your web browser and navigate to **[http://localhost:8080](http://localhost:8080)**. You will see your dashboard and can click through the repositories to view the raw code extracted from the Git database.

---

## 📈 Scaling to Production (Next Steps)

This project is a highly capable core engine. To scale this into an enterprise-grade platform like GitHub or GitLab, the following architectural upgrades are recommended:

### 1. Database Integration (Dynamic Identity)
* **Current State:** Reads a flat `authorized_keys` file into memory.
* **Production State:** Replace `os.ReadFile` in the `PublicKeyCallback` with a PostgreSQL connection. Query a `users` table by public key fingerprint to route connections dynamically without restarting the server.

### 2. Isolation & Permissions (Security)
* **Current State:** Git commands execute under the permissions of the OS user running the Go binary.
* **Production State:** 
  * Run the Go binary under a highly restricted, dedicated Linux user (e.g., `git`).
  * Implement repository-level permissions (Read/Write access checks) in the Go routing logic before wiring the `exec.Command` streams.

### 3. Web UI Authentication
* **Current State:** The Web UI on port 8080 is public to anyone on the network.
* **Production State:** Implement JWT or Session-based authentication on the `net/http` routes, linking Web UI sessions to the database users.

### 4. Reverse Proxying
* **Current State:** Go serves HTTP directly.
* **Production State:** Place the web server behind **Nginx** or **Caddy** to handle SSL/TLS termination (HTTPS), static asset caching, and request rate-limiting.

### 5. Efficient Git Interfacing
* **Current State:** Spawns an `os/exec` process for every Web UI click to read files.
* **Production State:** Integrate `libgit2` (via `git2go`) to read the Git object database directly in memory, drastically reducing process overhead for the Web visualizer.
```

```