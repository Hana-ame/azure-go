# Database Architecture Analysis: Azure-Go Storage Mapping

**Date:** 2026-07-10  
**Version:** `v26.07.10`  
**Scope:** Local SQLite Persistence (`hashes.db` & `paths.db`)

---

## 1. Overview

To implement an S3-compatible layer on top of Azure Drive with efficient deduplication, the system utilizes local SQLite databases to maintain a mapping between logical S3 paths, content hashes, and Azure physical resource IDs. This eliminates reliance on unstable Azure paths and enables "one-to-many" physical-to-logical mapping.

---

## 2. Table Analysis: `hashes` (Content Deduplication)

Stored in `hashes.db`, this table tracks the first occurrence of unique content.

### 2.1 Schema
| Column | Type | Constraint | Description |
| :--- | :--- | :--- | :--- |
| `md5` | TEXT | PRIMARY KEY | MD5 hash of the file content |
| `path` | TEXT | NOT NULL | The first S3 path that uploaded this content |

### 2.2 Purpose & Logic
The `hashes` table is used exclusively for **Content-Addressable Storage (CAS)**.

*   **During `Put`**: 
    1. The system calculates the MD5 hash of the incoming stream.
    2. It queries `SELECT path FROM hashes WHERE md5 = ?`.
    3. **If found**: The content is a duplicate. The system deletes the current upload and redirects the logical path to the existing resource.
    4. **If not found**: The content is unique. The hash and the current path are stored: `INSERT INTO hashes (md5, path)`.

---

## 3. Table Analysis: `paths` (Logical-to-Physical Mapping)

Stored in `paths.db`, this table decouples the S3 API from Azure's internal structure.

### 3.1 Schema
| Column | Type | Constraint | Description |
| :--- | :--- | :--- | :--- |
| `path` | TEXT | PRIMARY KEY | The logical S3 path (e.g., `bucket/key`) |
| `id` | TEXT | NOT NULL | The permanent Azure Resource ID |

### 3.2 Purpose & Logic
The `paths` table provides a **Fast Path** for resource retrieval and ensures stability after deduplication.

*   **Storage**: Every successful upload stores the returned Azure `ID` against the `path`.
*   **Retrieval (`Get`/`Head`/`Delete`)**: 
    Instead of asking Azure "Where is the file at this path?", the system asks the local DB "What is the ID for this path?".
    - **Step 1**: `SELECT id FROM paths WHERE path = ?`
    - **Step 2**: Use `id` to call Azure Graph API `/items/{id}/content`.
*   **Benefit**: ID-based access is faster, atomic, and immune to path changes.

---

## 4. Combined Workflow: The "Deduplication Loop"

The true power of the system emerges when both tables interact during a `Put` operation.

### Scenario: Uploading File B which is a duplicate of File A

1.  **Physical Upload**: File B is uploaded $\to$ Azure returns `ID_B`.
2.  **Path Mapping**: Local DB records `Path_B` $\to$ `ID_B`.
3.  **Hash Lookup**: `hashes` table reveals that `MD5_B` already exists at `Path_A`.
4.  **Physical Cleanup**: The system calls `Agent.DeleteByPath(Path_B)`, removing `ID_B` from Azure to save space.
5.  **ID Redirection**:
    *   System looks up `Path_A` in the `paths` table $\to$ finds `ID_A`.
    *   System updates `paths` table: `Path_B` $\to$ `ID_A`.
6.  **Final State**: 
    - `hashes`: `MD5_A` $\to$ `Path_A`
    - `paths`: `Path_A` $\to$ `ID_A` **AND** `Path_B` $\to$ `ID_A`

**Result**: The user can access the same content via two different S3 paths, but only one copy exists physically in Azure.

---

## 5. Complexity Analysis

| Operation | Time Complexity (Local) | Space Complexity | Note |
| :--- | :--- | :--- | :--- |
| **Lookup Path** | $O(1)$ (Indexed) | $O(N)$ | $N$ = Number of files |
| **Deduplication Check** | $O(1)$ (Indexed) | $O(U)$ | $U$ = Number of unique files |
| **S3 $\to$ Azure ID** | $O(1)$ | Constant | Direct ID access |
