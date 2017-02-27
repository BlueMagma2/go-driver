//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Ewout Prangsma
//

package driver

import (
	"context"
	"fmt"
	"path"
	"reflect"
)

// newDatabase creates a new Database implementation.
func newCollection(name string, db *database) (Collection, error) {
	if name == "" {
		return nil, WithStack(InvalidArgumentError{Message: "name is empty"})
	}
	if db == nil {
		return nil, WithStack(InvalidArgumentError{Message: "db is nil"})
	}
	return &collection{
		name: name,
		db:   db,
		conn: db.conn,
	}, nil
}

type collection struct {
	name string
	db   *database
	conn Connection
}

// relPath creates the relative path to this collection (`_db/<db-name>/_api/<api-name>/<col-name>`)
func (c *collection) relPath(apiName string) string {
	return path.Join(c.db.relPath(), "_api", apiName, c.name)
}

// Name returns the name of the collection.
func (c *collection) Name() string {
	return c.name
}

// Remove removes the entire collection.
// If the collection does not exist, a NotFoundError is returned.
func (c *collection) Remove(ctx context.Context) error {
	req, err := c.conn.NewRequest("DELETE", c.relPath("collection"))
	if err != nil {
		return WithStack(err)
	}
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return WithStack(err)
	}
	if err := resp.CheckStatus(200); err != nil {
		return WithStack(err)
	}
	return nil
}

// ReadDocument reads a single document with given key from the collection.
// The document data is stored into result, the document meta data is returned.
// If no document exists with given key, a NotFoundError is returned.
func (c *collection) ReadDocument(ctx context.Context, key string, result interface{}) (DocumentMeta, error) {
	if err := validateKey(key); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	req, err := c.conn.NewRequest("GET", path.Join(c.relPath("document"), key))
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if err := resp.CheckStatus(200); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse metadata
	var meta DocumentMeta
	if err := resp.ParseBody("", &meta); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse result
	if result != nil {
		if err := resp.ParseBody("", result); err != nil {
			return meta, WithStack(err)
		}
	}
	return meta, nil
}

// CreateDocument creates a single document in the collection.
// The document data is loaded from the given document, the document meta data is returned.
// If the document data already contains a `_key` field, this will be used as key of the new document,
// otherwise a unique key is created.
// A ConflictError is returned when a `_key` field contains a duplicate key, other any other field violates an index constraint.
// To return the NEW document, prepare a context with `WithReturnNew`.
// To wait until document has been synced to disk, prepare a context with `WithWaitForSync`.
func (c *collection) CreateDocument(ctx context.Context, document interface{}) (DocumentMeta, error) {
	if document == nil {
		return DocumentMeta{}, WithStack(InvalidArgumentError{Message: "document nil"})
	}
	req, err := c.conn.NewRequest("POST", c.relPath("document"))
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if _, err := req.SetBody(document); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if err := resp.CheckStatus(cs.okStatus(201, 202)); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if cs.Silent {
		// Empty response, we're done
		return DocumentMeta{}, nil
	}
	// Parse metadata
	var meta DocumentMeta
	if err := resp.ParseBody("", &meta); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse returnNew (if needed)
	if cs.ReturnNew != nil {
		if err := resp.ParseBody("new", cs.ReturnNew); err != nil {
			return meta, WithStack(err)
		}
	}
	return meta, nil
}

// CreateDocuments creates multiple documents in the collection.
// The document data is loaded from the given documents slice, the documents meta data is returned.
// If a documents element already contains a `_key` field, this will be used as key of the new document,
// otherwise a unique key is created.
// If a documents element contains a `_key` field with a duplicate key, other any other field violates an index constraint,
// a ConflictError is returned in its inded in the errors slice.
// To return the NEW documents, prepare a context with `WithReturnNew`. The data argument passed to `WithReturnNew` must be
// a slice with the same number of entries as the `documents` slice.
// To wait until document has been synced to disk, prepare a context with `WithWaitForSync`.
// If the create request itself fails or one of the arguments is invalid, an error is returned.
func (c *collection) CreateDocuments(ctx context.Context, documents interface{}) (DocumentMetaSlice, ErrorSlice, error) {
	documentsVal := reflect.ValueOf(documents)
	switch documentsVal.Kind() {
	case reflect.Array, reflect.Slice:
		// OK
	default:
		return nil, nil, WithStack(InvalidArgumentError{Message: fmt.Sprintf("documents data must be of kind Array, got %s", documentsVal.Kind())})
	}
	documentCount := documentsVal.Len()
	req, err := c.conn.NewRequest("POST", c.relPath("document"))
	if err != nil {
		return nil, nil, WithStack(err)
	}
	if _, err := req.SetBody(documents); err != nil {
		return nil, nil, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return nil, nil, WithStack(err)
	}
	if status := resp.StatusCode(); status != cs.okStatus(201, 202) {
		return nil, nil, WithStack(newArangoError(status, 0, "Invalid status"))
	}
	if cs.Silent {
		// Empty response, we're done
		return nil, nil, nil
	}
	// Parse response array
	metas, errs, err := parseResponseArray(resp, documentCount, cs)
	if err != nil {
		return nil, nil, WithStack(err)
	}
	return metas, errs, nil
}

// UpdateDocument updates a single document with given key in the collection.
// The document meta data is returned.
// To return the NEW document, prepare a context with `WithReturnNew`.
// To return the OLD document, prepare a context with `WithReturnOld`.
// To wait until document has been synced to disk, prepare a context with `WithWaitForSync`.
// If no document exists with given key, a NotFoundError is returned.
func (c *collection) UpdateDocument(ctx context.Context, key string, update interface{}) (DocumentMeta, error) {
	if err := validateKey(key); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if update == nil {
		return DocumentMeta{}, WithStack(InvalidArgumentError{Message: "update nil"})
	}
	req, err := c.conn.NewRequest("PATCH", path.Join(c.relPath("document"), key))
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if _, err := req.SetBody(update); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if err := resp.CheckStatus(cs.okStatus(201, 202)); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if cs.Silent {
		// Empty response, we're done
		return DocumentMeta{}, nil
	}
	// Parse metadata
	var meta DocumentMeta
	if err := resp.ParseBody("", &meta); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse returnOld (if needed)
	if cs.ReturnOld != nil {
		if err := resp.ParseBody("old", cs.ReturnOld); err != nil {
			return meta, WithStack(err)
		}
	}
	// Parse returnNew (if needed)
	if cs.ReturnNew != nil {
		if err := resp.ParseBody("new", cs.ReturnNew); err != nil {
			return meta, WithStack(err)
		}
	}
	return meta, nil
}

// UpdateDocuments updates multiple document with given keys in the collection.
// The updates are loaded from the given updates slice, the documents meta data are returned.
// To return the NEW documents, prepare a context with `WithReturnNew` with a slice of documents.
// To return the OLD documents, prepare a context with `WithReturnOld` with a slice of documents.
// To wait until documents has been synced to disk, prepare a context with `WithWaitForSync`.
// If no document exists with a given key, a NotFoundError is returned at its errors index.
func (c *collection) UpdateDocuments(ctx context.Context, keys []string, updates interface{}) (DocumentMetaSlice, ErrorSlice, error) {
	updatesVal := reflect.ValueOf(updates)
	switch updatesVal.Kind() {
	case reflect.Array, reflect.Slice:
		// OK
	default:
		return nil, nil, WithStack(InvalidArgumentError{Message: fmt.Sprintf("updates data must be of kind Array, got %s", updatesVal.Kind())})
	}
	updateCount := updatesVal.Len()
	if len(keys) != updateCount {
		return nil, nil, WithStack(InvalidArgumentError{Message: fmt.Sprintf("expected %d keys, got %s", updateCount, len(keys))})
	}
	for _, key := range keys {
		if err := validateKey(key); err != nil {
			return nil, nil, WithStack(err)
		}
	}
	req, err := c.conn.NewRequest("PATCH", c.relPath("document"))
	if err != nil {
		return nil, nil, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	mergeArray, err := createMergeArray(keys, cs.Revisions)
	if err != nil {
		return nil, nil, WithStack(err)
	}
	if _, err := req.SetBodyArray(updates, mergeArray); err != nil {
		return nil, nil, WithStack(err)
	}
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return nil, nil, WithStack(err)
	}
	if status := resp.StatusCode(); status != cs.okStatus(201, 202) {
		return nil, nil, WithStack(newArangoError(status, 0, "Invalid status"))
	}
	if cs.Silent {
		// Empty response, we're done
		return nil, nil, nil
	}
	// Parse response array
	metas, errs, err := parseResponseArray(resp, updateCount, cs)
	if err != nil {
		return nil, nil, WithStack(err)
	}
	return metas, errs, nil
}

// ReplaceDocument replaces a single document with given key in the collection with the document given in the document argument.
// The document meta data is returned.
// To return the NEW document, prepare a context with `WithReturnNew`.
// To return the OLD document, prepare a context with `WithReturnOld`.
// To wait until document has been synced to disk, prepare a context with `WithWaitForSync`.
// If no document exists with given key, a NotFoundError is returned.
func (c *collection) ReplaceDocument(ctx context.Context, key string, document interface{}) (DocumentMeta, error) {
	if err := validateKey(key); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if document == nil {
		return DocumentMeta{}, WithStack(InvalidArgumentError{Message: "document nil"})
	}
	req, err := c.conn.NewRequest("PUT", path.Join(c.relPath("document"), key))
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if _, err := req.SetBody(document); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if err := resp.CheckStatus(cs.okStatus(201, 202)); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if cs.Silent {
		// Empty response, we're done
		return DocumentMeta{}, nil
	}
	// Parse metadata
	var meta DocumentMeta
	if err := resp.ParseBody("", &meta); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse returnOld (if needed)
	if cs.ReturnOld != nil {
		if err := resp.ParseBody("old", cs.ReturnOld); err != nil {
			return meta, WithStack(err)
		}
	}
	// Parse returnNew (if needed)
	if cs.ReturnNew != nil {
		if err := resp.ParseBody("new", cs.ReturnNew); err != nil {
			return meta, WithStack(err)
		}
	}
	return meta, nil
}

// RemoveDocument removes a single document with given key from the collection.
// The document meta data is returned.
// To return the OLD document, prepare a context with `WithReturnOld`.
// To wait until removal has been synced to disk, prepare a context with `WithWaitForSync`.
// If no document exists with given key, a NotFoundError is returned.
func (c *collection) RemoveDocument(ctx context.Context, key string) (DocumentMeta, error) {
	if err := validateKey(key); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	req, err := c.conn.NewRequest("DELETE", path.Join(c.relPath("document"), key))
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	cs := applyContextSettings(ctx, req)
	resp, err := c.conn.Do(ctx, req)
	if err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if err := resp.CheckStatus(cs.okStatus(200, 202)); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	if cs.Silent {
		// Empty response, we're done
		return DocumentMeta{}, nil
	}
	// Parse metadata
	var meta DocumentMeta
	if err := resp.ParseBody("", &meta); err != nil {
		return DocumentMeta{}, WithStack(err)
	}
	// Parse returnOld (if needed)
	if cs.ReturnOld != nil {
		if err := resp.ParseBody("old", cs.ReturnOld); err != nil {
			return meta, WithStack(err)
		}
	}
	return meta, nil
}

// createMergeArray returns an array of metadata maps with `_key` and/or `_rev` elements.
func createMergeArray(keys, revs []string) ([]map[string]interface{}, error) {
	if keys == nil && revs == nil {
		return nil, nil
	}
	if revs == nil {
		mergeArray := make([]map[string]interface{}, len(keys))
		for i, k := range keys {
			mergeArray[i] = map[string]interface{}{
				"_key": k,
			}
		}
		return mergeArray, nil
	}
	if keys == nil {
		mergeArray := make([]map[string]interface{}, len(revs))
		for i, r := range revs {
			mergeArray[i] = map[string]interface{}{
				"_rev": r,
			}
		}
		return mergeArray, nil
	}
	if len(keys) != len(revs) {
		return nil, WithStack(InvalidArgumentError{Message: fmt.Sprintf("#keys must be equal to #revs, got %d, %d", len(keys), len(revs))})
	}
	mergeArray := make([]map[string]interface{}, len(keys))
	for i, k := range keys {
		mergeArray[i] = map[string]interface{}{
			"_key": k,
			"_rev": revs[i],
		}
	}
	return mergeArray, nil

}

// parseResponseArray parses an array response in the given response
func parseResponseArray(resp Response, count int, cs contextSettings) (DocumentMetaSlice, ErrorSlice, error) {
	resps, err := resp.ParseArrayBody()
	if err != nil {
		return nil, nil, WithStack(err)
	}
	metas := make(DocumentMetaSlice, count)
	errs := make(ErrorSlice, count)
	returnOldVal := reflect.ValueOf(cs.ReturnOld)
	returnNewVal := reflect.ValueOf(cs.ReturnNew)
	for i := 0; i < count; i++ {
		resp := resps[i]
		var meta DocumentMeta
		if err := resp.CheckStatus(200, 201, 202); err != nil {
			errs[i] = err
		} else {
			if err := resp.ParseBody("", &meta); err != nil {
				errs[i] = err
			} else {
				metas[i] = meta
				// Parse returnOld (if needed)
				if cs.ReturnOld != nil {
					returnOldEntryVal := returnOldVal.Index(i).Addr()
					if err := resp.ParseBody("old", returnOldEntryVal.Interface()); err != nil {
						errs[i] = err
					}
				}
				// Parse returnNew (if needed)
				if cs.ReturnNew != nil {
					returnNewEntryVal := returnNewVal.Index(i).Addr()
					if err := resp.ParseBody("new", returnNewEntryVal.Interface()); err != nil {
						errs[i] = err
					}
				}
			}
		}
	}
	return metas, errs, nil
}
