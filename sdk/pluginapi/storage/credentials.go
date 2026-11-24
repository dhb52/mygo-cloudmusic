package storage

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// Credential stores opaque bytes in the system credential store. Development
// builds use a private file in the app's data directory.
type Credential struct {
	key  string
	path func() (string, error)
}

var serviceOverride *string

// UseCredentialService sets the credential namespace before the plugin starts.
// An empty service uses the host profile's development credential files.
func UseCredentialService(service string) {
	serviceOverride = &service
}

func credentialStore() (mygo.SecretsModule, bool) {
	if serviceOverride != nil {
		service := *serviceOverride
		return mygo.Secrets.Service(service), service != ""
	}
	return mygo.Secrets, !mygo.IsDev()
}

func (s Scope) Credential(name string) Credential {
	if s.id == "" || !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." {
		return Credential{}
	}
	return Credential{
		key: "plugin:" + s.directory() + ":" + name,
		path: func() (string, error) {
			return s.DataPath(filepath.Join("credentials", name))
		},
	}
}

// LegacyCredential uses the original instance-prefixed key. file optionally
// supplies a development filename that predates the credential key.
func (s Scope) LegacyCredential(name, file string) Credential {
	if s.id == "" || name == "" {
		return Credential{}
	}
	key := string(s.id) + "-" + name
	if file == "" {
		file = key
	}
	return Credential{key: key, path: func() (string, error) { return LegacyDataPath(file) }}
}

func (c Credential) Read() ([]byte, error) {
	secrets, system := credentialStore()
	if !system {
		return c.ReadFile()
	}
	return secrets.Get(c.key)
}

func (c Credential) Write(data []byte) error {
	secrets, system := credentialStore()
	if !system {
		return c.WriteFile(data)
	}
	return secrets.Set(c.key, data)
}

func (c Credential) Delete() error {
	secrets, system := credentialStore()
	if !system {
		return c.DeleteFile()
	}
	return secrets.Delete(c.key)
}

// ReadFile reads the private file for a plugin that supports an existing file fallback.
func (c Credential) ReadFile() ([]byte, error) {
	if c.path == nil {
		return nil, fs.ErrInvalid
	}
	path, err := c.path()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteFile replaces the private file after the complete record has been written.
func (c Credential) WriteFile(data []byte) error {
	if c.path == nil {
		return fs.ErrInvalid
	}
	path, err := c.path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".credential-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (c Credential) DeleteFile() error {
	if c.path == nil {
		return fs.ErrInvalid
	}
	path, err := c.path()
	if err != nil {
		return err
	}
	return os.Remove(path)
}
