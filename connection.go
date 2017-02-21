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
	"io"
)

// Connection is a connenction to a database server using a specific protocol.
type Connection interface {
	// NewRequest creates a new request with given method and path.
	NewRequest(method, path string) (Request, error)

	// Do performs a given request, returning its response.
	Do(ctx context.Context, req Request) (Response, error)
}

// Request represents the input to a request on the server.
type Request interface {
	// SetQuery sets a single query argument of the request.
	SetQuery(key, value string) Request
	// SetBody sets the content of the request.
	// The protocol of the connection determines what kinds of marshalling is taking place.
	SetBody(body interface{}) Request
	// SetHeader sets a single header arguments of the request.
	SetHeader(key, value string) Request
}

// Response represents the response from the server on a given request.
type Response interface {
	// StatusCode returns an HTTP compatible status code of the response.
	StatusCode() int
	// CheckStatus checks if the status of the response equals to one of the given status codes.
	// If so, nil is returned.
	// If not, an attempt is made to parse an error response in the body and an error is returned.
	CheckStatus(validStatusCodes ...int) error
	// Body returns a reader for accessing the content of the response.
	// Clients have to close this body.
	Body() io.ReadCloser
	// ParseBody performs protocol specific unmarshalling of the response data into the given result.
	ParseBody(result interface{}) error
}
