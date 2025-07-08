
# Optimizing Upload and Download Performance in Materials Commons CLI

Based on my analysis of the codebase, here are comprehensive recommendations to speed up the upload and download processes:

## 1. Parallel Processing Implementation

### For Upload (`up.py`)
```python
# Add to standard_upload_v2 in tree_functions.py
from concurrent.futures import ThreadPoolExecutor, as_completed

def parallel_upload_files(proj, file_paths, working_dir, **kwargs):
    results = {'files': {}, 'errors': {}}
    
    def upload_single_file(path):
        file_result, error_msg = check_and_upload_file(
            proj, path, working_dir, **kwargs)
        return path, file_result, error_msg
    
    with ThreadPoolExecutor(max_workers=10) as executor:
        futures = [executor.submit(upload_single_file, path) for path in file_paths]
        for future in as_completed(futures):
            path, file_result, error_msg = future.result()
            if file_result:
                results['files'][path] = file_result
            if error_msg:
                results['errors'][path] = error_msg
                
    return results
```

### For Download (`down.py`)
```python
# Add to standard_download in down.py
from concurrent.futures import ThreadPoolExecutor

def parallel_download_files(proj, file_data, working_dir, **kwargs):
    results = []
    
    def download_single_file(file_info):
        path = file_info['path']
        file_id = file_info['id']
        local_path = filefuncs.make_local_abspath(proj.local_path, path)
        result = _check_download_file(
            proj.id, file_id, local_path, proj.remote, working_dir, **kwargs)
        return path, result
    
    with ThreadPoolExecutor(max_workers=10) as executor:
        futures = [executor.submit(download_single_file, file_data[path]) 
                  for path in file_data if file_data[path]['r_type'] == 'file']
        for future in as_completed(futures):
            path, result = future.result()
            results.append((path, result))
            
    return results
```

## 2. Enhanced Caching Strategy

### Improve LocalTree and RemoteTree
```python
# Add to LocalTree class in treedb.py
def bulk_update(self, paths):
    """Update multiple paths at once to reduce DB operations"""
    records = []
    checktime = time.time()
    for path in paths:
        local_abspath = os.path.join(self.proj_local_path, path.lstrip('/'))
        if os.path.exists(local_abspath):
            records.append(self._make_record(local_abspath, checktime))
    
    # Use a single transaction for all updates
    with self.conn:
        for record in records:
            self.insert_or_replace(record, verbose=False)
```

### Optimize File Change Detection
```python
# Add to treedb.py
def get_changed_files(self, last_sync_time):
    """Get only files that have changed since last sync"""
    changed = []
    for root, dirs, files in os.walk(self.proj_local_path):
        for file in files:
            path = os.path.join(root, file)
            if os.path.getmtime(path) > last_sync_time:
                changed.append(path)
    return changed
```

## 3. Server-Side Optimizations

### Database Schema Improvements
The server's `files` table should include:
- Indexes on frequently queried columns (path, project_id, directory_id)
- A last_modified timestamp for efficient change detection
- Consider adding a dedicated table for tracking file changes

### API Endpoint Enhancements
Add new endpoints to the server:
1. `GET /projects/{id}/changes?since={timestamp}` - Returns only files changed since timestamp
2. `POST /projects/{id}/bulk-upload` - Accepts multiple files in a single request
3. `POST /projects/{id}/bulk-download` - Prepares multiple files for download as a single operation

### Example Server-Side Code (PHP/Laravel)
```php
// New controller method for getting changes
public function getChanges(Request $request, $projectId)
{
    $since = $request->input('since', 0);
    
    return File::where('project_id', $projectId)
        ->where('updated_at', '>', date('Y-m-d H:i:s', $since))
        ->get();
}

// Bulk upload endpoint
public function bulkUpload(Request $request, $projectId)
{
    // Process multiple files in a single transaction
    DB::transaction(function() use ($request, $projectId) {
        foreach($request->file('files') as $file) {
            // Process each file
        }
    });
}
```

## 4. Incremental Sync Implementation

Implement a more efficient sync algorithm that:
1. Tracks the last sync time in the project config
2. Only processes files that have changed since the last sync
3. Uses server-side change detection when available

```python
# Add to up.py and down.py
def incremental_sync(proj, paths, working_dir, **kwargs):
    pconfig = clifuncs.read_project_config(proj.local_path)
    last_sync = pconfig.last_sync_time or 0
    
    # Get changed files from server
    if hasattr(proj.remote, 'get_changes'):
        remote_changes = proj.remote.get_changes(proj.id, since=last_sync)
    else:
        # Fall back to full comparison if server doesn't support change detection
        remote_changes = None
    
    # Get local changes
    localtree = LocalTree(proj.local_path)
    local_changes = localtree.get_changed_files(last_sync)
    
    # Process only changed files
    # [implementation details]
    
    # Update last sync time
    pconfig.last_sync_time = time.time()
    pconfig.save()
```

## 5. Chunked File Transfers

For large files, implement chunked uploads/downloads:

```python
def chunked_upload(proj, file_path, chunk_size=8*1024*1024):
    """Upload large files in chunks to avoid memory issues"""
    file_size = os.path.getsize(file_path)
    
    with open(file_path, 'rb') as f:
        # Create upload session
        session = proj.remote.create_upload_session(proj.id, os.path.basename(file_path), file_size)
        
        # Upload chunks
        offset = 0
        while offset < file_size:
            chunk = f.read(chunk_size)
            proj.remote.upload_chunk(session.id, chunk, offset)
            offset += len(chunk)
            
        # Complete upload
        return proj.remote.complete_upload(session.id)
```

## Implementation Priority

1. **Immediate wins**: Implement parallel processing for file uploads/downloads
2. **Medium effort**: Enhance the caching strategy with bulk operations
3. **Coordination required**: Work with server team to implement the API enhancements
4. **Long-term**: Implement incremental sync and chunked file transfers

These improvements should significantly reduce the time required for uploads and downloads, especially for projects with many files or large directory structures.