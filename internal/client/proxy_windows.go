//go:build windows

package client

import (
	"fmt"

	"github.com/QingYu-Su/Yui/pkg/wauth"
)

func additionalHeaders(proxy string, req []string) []string {
	req = append(req, fmt.Sprintf("Proxy-Authorization: %s", wauth.GetAuthorizationHeader(proxy)))
	return req
}
