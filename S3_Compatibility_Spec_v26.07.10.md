# Specification: S3 Compatibility Layer & System Diagnostics (v26.07.10)

**Date:** 2026-07-10  
**Version:** `v26.07.10`  
**Scope:** Storage Driver, Local Mapping Layer, Azure Agent, System Initialization

---

## 1. Overview

This update introduces a local mapping layer to decouple S3 logical paths from Azure physical storage. By migrating from **Path-based Access** to **ID-based Access**, the system resolves inconsistencies caused by content deduplication and improves the reliability of resource retrieval. Additionally, a comprehensive diagnostic system is implemented to eliminate "silent failures" during automated deployment.

---

## 2. Local Mapping Specification (`PathStore`)

To ensure that S3 paths remain valid even after physical files are moved or deleted (due to deduplication), a local persistence layer is implemented via `SQLitePathStore`.

### 2.1 Data Schema
The mapping is stored in a local SQLite database (`paths.db`) using the following schema:

**Table: `paths`**
| Column | Type | Constraint | Description |
| :--- | :--- | :--- | :--- |
| `path` | TEXT | PRIMARY KEY | The logical S3 path (e.g., `bucket/key`) |
| `id` | TEXT | NOT NULL | The unique Azure Resource ID |

### 2.2 Interface Methods

#### `Lookup(path string) (string, error)`
**Purpose:** Retrieve the Azure ID associated with the S3 path.

```go
func (s *SQLitePathStore) Lookup(path string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM paths WHERE path = ?`, path).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}
```
**Dependency Chain:** `sql.DB.QueryRow` $\to$ `Scan`

**Conditional Branches:**
- `sql.ErrNoRows` $\to$ Return `"", nil` (Not found)
- `err != nil` $\to$ Return `"", err`

#### `Store(path, id string) error`
**Purpose:** Map an S3 path to an Azure ID.

```go
func (s *SQLitePathStore) Store(path, id string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO paths (path, id) VALUES (?, ?)`, path, id)
	return err
}
```
**Dependency Chain:** `sql.DB.Exec`

#### `Delete(path string) error`
**Purpose:** Remove the mapping for a given path.

```go
func (s *SQLitePathStore) Delete(path string) error {
	_, err := s.db.Exec(`DELETE FROM paths WHERE path = ?`, path)
	return err
}
```
**Dependency Chain:** `sql.DB.Exec`

---

## 3. Driver Logic Flow (`AzureDriver`)

The `AzureDriver` orchestrates the interaction between the S3 Controller, the local `PathStore`, and the Azure `Agent`.

### 3.1 `Put(path, contentType string, body io.Reader)`
**Purpose:** Upload a file, handle deduplication, and persist the ID mapping.

```go
func (d *AzureDriver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	// ... hash calculation ...
	id, err := d.agent.Put(path, contentType, tee)
	if err != nil { return nil, err }

	if d.pathStore != nil {
		_ = d.pathStore.Store(path, id)
	}

	if d.hashStore != nil {
		if existingPath, err := d.hashStore.Lookup(md5hex); err == nil && existingPath != "" {
			d.agent.DeleteByPath(path)
			if d.pathStore != nil {
				if existingID, err := d.pathStore.Lookup(existingPath); err == nil && existingID != "" {
					_ = d.pathStore.Store(path, existingID)
				}
			}
			return &storage.PutResult{ID: id, ETag: etag, DupPath: existingPath}, nil
		}
		_ = d.hashStore.Store(md5hex, path)
	}
	return &storage.PutResult{ID: id, ETag: etag}, nil
}
```
**Dependency Chain:** `Agent.Put` $\to$ `PathStore.Store` $\to$ `HashStore.Lookup` $\to$ `Agent.DeleteByPath` $\to$ `PathStore.Lookup` $\to$ `PathStore.Store`

**Conditional Branches:**
- `Agent.Put` fails $\to$ Return error.
- `HashStore` hit $\to$ Delete physical copy $\to$ Update `PathStore` to point to `existingID`.

### 3.2 `Get(path string)`
**Purpose:** Retrieve content, prioritizing ID-based access.

```go
func (d *AzureDriver) Get(path string) (io.ReadCloser, int64, string, error) {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			return d.agent.GetByID(id)
		}
	}
	return d.agent.GetByPath(path)
}
```
**Dependency Chain:** `PathStore.Lookup` $\to$ `Agent.GetByID` (Hit) OR `Agent.GetByPath` (Miss)

### 3.3 `Head(path string)`
**Purpose:** Retrieve metadata, prioritizing ID-based access.

```go
func (d *AzureDriver) Head(path string) (int64, string, error) {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			return d.agent.HeadByID(id)
		}
	}
	return d.agent.HeadByPath(path)
}
```
**Dependency Chain:** `PathStore.Lookup` $\to$ `Agent.HeadByID` (Hit) OR `Agent.HeadByPath` (Miss)

### 3.4 `Delete(path string)`
**Purpose:** Delete resource, prioritizing ID-based removal.

```go
func (d *AzureDriver) Delete(path string) error {
	if d.pathStore != nil {
		if id, err := d.pathStore.Lookup(path); err == nil && id != "" {
			_ = d.pathStore.Delete(path)
			return d.agent.DeleteByID(id)
		}
	}
	return d.agent.DeleteByPath(path)
}
```
**Dependency Chain:** `PathStore.Lookup` $\to$ `PathStore.Delete` $\to$ `Agent.DeleteByID` (Hit) OR `Agent.DeleteByPath` (Miss)

---

## 4. Backend Agent Extensions

Extensions to the `Agent` struct to allow direct operations via the Azure Graph API `items` endpoint.

### 4.1 `GetByID(id string)`
```go
func (agt *Agent) GetByID(id string) (io.ReadCloser, int64, string, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id + `/content`
	resp, err := myfetch.Fetch(http.MethodGet, endpoint, map[string]string{"Authorization": "Bearer " + agt.access_token}, nil)
	// ... handle response ...
}
```
**Dependency Chain:** `myfetch.Fetch` $\to$ `resp.Body`

### 4.2 `HeadByID(id string)`
```go
func (agt *Agent) HeadByID(id string) (int64, string, error) {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id + `/content`
	resp, err := myfetch.Fetch(http.MethodHead, endpoint, map[string]string{"Authorization": "Bearer " + agt.access_token}, nil)
	// ... return resp.ContentLength and Content-Type ...
}
```
**Dependency Chain:** `myfetch.Fetch` $\to$ `resp.ContentLength` $\to$ `resp.Header.Get("Content-Type")`

### 4.3 `DeleteByID(id string)`
```go
func (agt *Agent) DeleteByID(id string) error {
	endpoint := `https://graph.microsoft.com/v1.0/me/drive/items/` + id
	_, err := myfetch.Fetch(http.MethodDelete, endpoint, map[string]string{"Authorization": "Bearer " + agt.access_token}, nil)
	return err
}
```
**Dependency Chain:** `myfetch.Fetch`

---

## 5. Diagnostic & Observability Specification

Implemented to eliminate "silent failures" during automated deployment on VPS.

### 5.1 Initialization Sequence
The `main()` function executes the following diagnostic flow:

```go
// 1. Version
log.Printf("[Init] Version: %s", Version)
// 2. Context
cwd, _ := os.Getwd()
log.Printf("[Init] Current Working Directory: %s", cwd)
// 3. Config Loading
if err := godotenv.Load("refresh_token"); err != nil { ... } else { log.Println("[Init] Successfully loaded...") }
if err := godotenv.Load("access"); err != nil { ... } else { log.Println("[Init] Successfully loaded...") }
overloadEnvByJsonFile(".json")
// 4. Agent state
log.Printf("[Init] Agent Config: tenent_id=%s, client_id=%s, scope=%s", agent.tenent_id, agent.client_id, agent.scope)
```
**Dependency Chain:** `os.Getwd` $\to$ `godotenv.Load` $\to$ `overloadEnvByJsonFile` $\to$ `os.Getenv`

### 5.2 Token Lifecycle Tracking

#### Initial Load
```go
log.Printf("[Token Loaded] access_token:\n%s", agent.access_token)
log.Printf("[Token Loaded] refresh_token:\n%s", agent.refresh_token)
```
**Purpose:** Immediate verification of token availability upon startup.

#### Token Renewal
```go
oldAccessToken := agt.access_token
// ... perform renewal ...
log.Printf("[Token Renewed] access_token:\nOLD: %s\nNEW: %s", oldAccessToken, agt.access_token)
```
**Dependency Chain:** `Agent.RenewToken` $\to$ `myfetch.Fetch` $\to$ `myfetch.ResponseToJson` $\to$ `godotenv.Write` $\to$ `log.Printf`
