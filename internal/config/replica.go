package config

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/zalando/go-keyring"
	"go.yaml.in/yaml/v3"
)

type Replica struct {
	URL        string `json:"url"`
	Endpoint   string `json:"endpoint,omitempty"`
	Region     string `json:"region,omitempty"`
	SecretFile bool   `json:"secret_file"`
}

type ReplicaCredential struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func (r Replica) Validate() error {
	u, err := url.Parse(r.URL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("replica URL must not include credentials, query or fragment")
	}
	switch u.Scheme {
	case "file":
		if u.Host != "" || !filepath.IsAbs(u.Path) || u.Path == "/" {
			return errors.New("file replica requires an absolute directory URL")
		}
		if r.Endpoint != "" {
			return errors.New("file replica does not accept an endpoint")
		}
	case "s3":
		if u.Host == "" || strings.Trim(u.Path, "/") == "" {
			return errors.New("S3 replica requires bucket and account-specific prefix")
		}
	default:
		return errors.New("replica must use file:// or s3://")
	}
	if r.Endpoint != "" {
		e, err := url.Parse(r.Endpoint)
		local := e != nil && e.Scheme == "http" && (e.Hostname() == "localhost" || e.Hostname() == "127.0.0.1")
		if err != nil || e.Host == "" || e.User != nil || e.RawQuery != "" || e.Fragment != "" || strings.Trim(e.Path, "/") != "" || e.Scheme != "https" && !local {
			return errors.New("endpoint must use HTTPS (localhost HTTP allowed for tests)")
		}
	}
	return nil
}

func (s Profiles) SaveReplica(p Profile, r Replica, c ReplicaCredential) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	dir, err := s.dir(p.Name)
	if err != nil {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	guard := flock.New(filepath.Join(dir, "replica.setup.lock"), flock.SetPermissions(0600))
	if err := guard.Lock(); err != nil {
		return errors.New("unable to lock replica configuration")
	}
	defer func() { _ = guard.Unlock() }()
	path := filepath.Join(dir, "replica.json")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("replica already configured; refusing overwrite")
	}
	if strings.HasPrefix(r.URL, "s3:") {
		if c.AccessKey == "" || c.SecretKey == "" {
			return errors.New("S3 access and secret keys are required")
		}
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if r.SecretFile {
			if err := writePrivate(filepath.Join(dir, "replica.credentials"), data); err != nil {
				return err
			}
		} else if err := keyring.Set("woovi-pix-mcp-replica", s.secretID(p.Name), string(data)); err != nil {
			return errors.New("replica OS credential vault unavailable; choose --secret-file explicitly if acceptable")
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, data)
}

func (s Profiles) LoadReplica(p Profile) (Replica, ReplicaCredential, error) {
	dir, err := s.dir(p.Name)
	if err != nil {
		return Replica{}, ReplicaCredential{}, err
	}
	data, err := readPrivate(filepath.Join(dir, "replica.json"))
	if err != nil {
		return Replica{}, ReplicaCredential{}, err
	}
	var r Replica
	if err := json.Unmarshal(data, &r); err != nil {
		return r, ReplicaCredential{}, errors.New("invalid replica metadata")
	}
	if err := r.Validate(); err != nil {
		return r, ReplicaCredential{}, err
	}
	var c ReplicaCredential
	if strings.HasPrefix(r.URL, "s3:") {
		if r.SecretFile {
			data, err = readPrivate(filepath.Join(dir, "replica.credentials"))
		} else {
			var text string
			text, err = keyring.Get("woovi-pix-mcp-replica", s.secretID(p.Name))
			data = []byte(text)
		}
		if err != nil || json.Unmarshal(data, &c) != nil || c.AccessKey == "" || c.SecretKey == "" {
			return r, c, errors.New("replica credentials unavailable")
		}
	}
	return r, c, nil
}

// RunReplica keeps Litestream optional and separate from the stdio server.
// Child output is deliberately suppressed because external errors can contain
// endpoints or credentials. No AppID is passed to the child.
func (s Profiles) RunReplica(ctx context.Context, p Profile, binary, action string, ack bool) error {
	if action != "replicate" && action != "status" && action != "restore" {
		return errors.New("invalid replica action")
	}
	r, c, err := s.LoadReplica(p)
	if err != nil {
		return err
	}
	version, err := exec.CommandContext(ctx, binary, "version").Output()
	text := strings.TrimSpace(string(version))
	patch, parseErr := strconv.Atoi(strings.TrimPrefix(text, "0.5."))
	if err != nil || !strings.HasPrefix(text, "0.5.") || parseErr != nil || patch < 12 {
		return errors.New("litestream 0.5.12+ executable is required within 0.5.x (validated with 0.5.17)")
	}
	db := s.DatabasePath(p)
	if err := privateDir(filepath.Dir(db)); err != nil {
		return err
	}
	runtime := flock.New(db+".runtime.lock", flock.SetPermissions(0600))
	var locked bool
	if action == "restore" {
		locked, err = runtime.TryLock()
	} else {
		locked, err = runtime.TryRLock()
	}
	if err != nil || !locked {
		return errors.New("stop MCP and replication before restoring")
	}
	defer func() { _ = runtime.Unlock() }()
	if action == "restore" {
		if !ack {
			return errors.New("restore requires --acknowledge-stale-state: reconcile provider history before re-enabling creation")
		}
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if _, err := os.Lstat(db + suffix); !errors.Is(err, os.ErrNotExist) {
				return errors.New("restore requires absent database and sidecars; archive existing state explicitly, never overwrite")
			}
		}
		if err := writePrivate(db+".recovered", []byte("Creation disabled: restored history may omit provider operations. Reconcile provider history before explicitly removing this marker.\n")); err != nil {
			return err
		}
	} else {
		info, err := os.Lstat(db)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("initialize a private SQLite database before replication")
		}
	}
	// Only one managed replication process per account/environment.
	if action == "replicate" {
		guard := flock.New(db+".replica.lock", flock.SetPermissions(0600))
		locked, err := guard.TryLock()
		if err != nil || !locked {
			return errors.New("replication is already running")
		}
		defer func() { _ = guard.Unlock() }()
	}
	replica := map[string]any{"url": r.URL}
	if strings.HasPrefix(r.URL, "s3:") {
		replica["access-key-id"] = "${WOOVI_REPLICA_ACCESS_KEY}"
		replica["secret-access-key"] = "${WOOVI_REPLICA_SECRET_KEY}"
		if r.Region != "" {
			replica["region"] = r.Region
		}
		if r.Endpoint != "" {
			replica["endpoint"] = r.Endpoint
			replica["force-path-style"] = true
		}
	}
	data, err := yaml.Marshal(map[string]any{"dbs": []any{map[string]any{"path": db, "replicas": []any{replica}}}})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(db), ".litestream-*.yml")
	if err != nil {
		return errors.New("unable to stage Litestream configuration")
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return errors.New("unable to write Litestream configuration")
	}
	if err := f.Close(); err != nil {
		return err
	}
	args := []string{action, "-config", f.Name()}
	if action == "status" {
		args = append(args, "-json", db)
	}
	if action == "restore" {
		args = append(args, "-integrity-check", "full", db)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "WOOVI_REPLICA_ACCESS_KEY="+c.AccessKey, "WOOVI_REPLICA_SECRET_KEY="+c.SecretKey)
	if action == "status" {
		cmd.Stdout = nil
		output, err := cmd.Output()
		if err != nil {
			return errors.New("unable to inspect local replica status")
		}
		var statuses []struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(output, &statuses); err != nil || len(statuses) != 1 {
			return errors.New("invalid local replica status response")
		}
		if statuses[0].Status != "ok" {
			return errors.New("local replica state is not initialized or healthy; this is not a remote freshness check")
		}
		return nil
	}
	if err := cmd.Run(); err != nil {
		return errors.New("litestream operation failed; no database overwrite was requested")
	}
	if action == "restore" {
		if err := os.Chmod(db, 0600); err != nil {
			return errors.New("unable to restrict restored database permissions")
		}
	}
	return nil
}
