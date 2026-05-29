package trafficpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/avast/retry-go/v4"
	"golang.org/x/sync/singleflight"
)

const (
	wellKnownOpenIDConfPath = "/.well-known/openid-configuration"
	userAgent               = "kgateway/oidc-discovery"
	oidcAcceptedContentType = "application/json"

	// oidcDiscoveryRetryInterval is the fixed period after which the reconciler
	// will re-examine a TrafficPolicy / GatewayExtension whose OIDC discovery
	// failed with a transient (network-layer) error. The value is intentionally
	// shorter than the 5-minute cache-refresh interval so the policy recovers
	// quickly once the IdP is reachable again.
	oidcDiscoveryRetryInterval = 30 * time.Second
)

// TransientDiscoveryError wraps an OIDC discovery failure that is caused by a
// transient network condition (connection refused, reset, EOF, DNS failure,
// timeout). The reconciler uses errors.As to distinguish this from a permanent
// configuration error (wrong issuer URL, invalid JSON, HTTP 404) and schedules
// a fixed RequeueAfter instead of marking the resource as permanently invalid.
type TransientDiscoveryError struct{ Cause error }

func (e *TransientDiscoveryError) Error() string {
	return fmt.Sprintf("OIDC discovery failed (transient): %v", e.Cause)
}

func (e *TransientDiscoveryError) Unwrap() error { return e.Cause }

// isTransientNetworkError reports whether err is a network-layer failure that
// warrants a retry rather than a permanent Invalid condition.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// io.EOF / io.ErrUnexpectedEOF — server closed the connection
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// syscall errors — ECONNRESET, ECONNREFUSED
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	// net.Error with Timeout flag set
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// *url.Error wrapping any of the above (returned by http.Client)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTransientNetworkError(urlErr.Err)
	}
	return false
}

type oidcProviderConfigDiscoverer struct {
	// caches oidcProviderConfig per issuer URI
	cache                sync.Map
	cacheRefreshInterval time.Duration
	// discoverGroup deduplicates concurrent discover() calls for the same issuer URI,
	// preventing redundant HTTP requests when the cache is cleared.
	discoverGroup singleflight.Group
}

// oidcProviderConfig maps the OpenID provider config response.
// Refer to https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderConfigurationResponse for more details.
type oidcProviderConfig struct {
	TokenEndpoint         string  `json:"token_endpoint"`
	AuthorizationEndpoint string  `json:"authorization_endpoint"`
	EndSessionEndpoint    *string `json:"end_session_endpoint,omitempty"`
	JWKSURI               string  `json:"jwks_uri"`
}

// newOIDCProviderConfigDiscoverer returns a oidcProviderConfigDiscoverer instance that is responsible
// for periodically refreshing the OpenID provider configuration cache
func newOIDCProviderConfigDiscoverer() *oidcProviderConfigDiscoverer {
	return &oidcProviderConfigDiscoverer{
		cacheRefreshInterval: 5 * time.Minute,
	}
}

// scheduleRetry evicts issuerURI from the cache after oidcDiscoveryRetryInterval so
// that the next get() call re-attempts discovery without waiting for the full 5-minute flush.
func (o *oidcProviderConfigDiscoverer) scheduleRetry(ctx context.Context, issuerURI string) {
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(oidcDiscoveryRetryInterval):
			o.cache.Delete(issuerURI)
		}
	}()
}

// refresh periodically clears the cache to allow re-discovery of OpenID provider configurations.
// The OpenID provider configuration is not expected to change frequently, so caching it for a longer duration
// is desirable to prevent excessive network calls. However, to accommodate potential changes in the provider configuration,
// the cache is cleared at regular intervals, prompting re-discovery on subsequent requests.
func (o *oidcProviderConfigDiscoverer) refresh(ctx context.Context) {
	ticker := time.NewTicker(o.cacheRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Guard against the race where both ctx.Done() and ticker.C are
			// ready simultaneously and the scheduler picks ticker.C first.
			if ctx.Err() != nil {
				return
			}
			// refresh the cache every 5 minutes; next get() will re-discover the config
			o.cache.Clear()
		}
	}
}

func (o *oidcProviderConfigDiscoverer) get(issuerURI string) (*oidcProviderConfig, error) {
	v, ok := o.cache.Load(issuerURI)
	if ok {
		return v.(*oidcProviderConfig), nil
	}

	// Use singleflight to deduplicate concurrent discovery calls for the same issuer.
	// After a cache.Clear(), multiple goroutines may call get() simultaneously;
	// singleflight ensures only one discover() HTTP request is made per issuer URI.
	result, err, _ := o.discoverGroup.Do(issuerURI, func() (any, error) {
		// Re-check the cache inside the singleflight function, as another caller
		// may have populated it between our initial Load and entering the group.
		if v, ok := o.cache.Load(issuerURI); ok {
			return v, nil
		}
		cfg, err := o.discover(issuerURI)
		if err != nil {
			return nil, err
		}
		o.cache.Store(issuerURI, cfg)
		return cfg, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*oidcProviderConfig), nil
}

func (o *oidcProviderConfigDiscoverer) discover(issuerURI string) (*oidcProviderConfig, error) {
	discoveryURL, err := url.Parse(issuerURI + wellKnownOpenIDConfPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing discovery URL: %w", err)
	}

	cfg := &oidcProviderConfig{}
	client := &http.Client{Timeout: 30 * time.Second}
	err = retry.Do(func() error {
		// TODO: allow using custom certs for HTTPS Issuer URI
		req, err := http.NewRequest(http.MethodGet, discoveryURL.String(), nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", oidcAcceptedContentType)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch OIDC configuration: %w", err)
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		// retry on specific 5xx status codes
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return fmt.Errorf("error discovering OpenID provider config; unexpected status code %d", resp.StatusCode)

		case http.StatusOK:
			if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
				return retry.Unrecoverable(fmt.Errorf("error decoding OpenID provider config: %w", err))
			}

		default:
			return retry.Unrecoverable(fmt.Errorf("error discovering OpenID provider config; unexpected status code %d", resp.StatusCode))
		}
		return nil
	}, retry.Attempts(5), retry.Delay(100*time.Millisecond), retry.MaxDelay(5*time.Second), retry.DelayType(retry.BackOffDelay))
	if err != nil {
		// Unwrap retry-go's aggregate error to inspect the underlying cause.
		// retry.Unrecoverable wraps permanent errors; everything else comes from
		// a retryable (network-layer) failure.
		cause := err
		var retryErrs retry.Error
		if errors.As(err, &retryErrs) && len(retryErrs) > 0 {
			cause = retryErrs[len(retryErrs)-1]
		}
		if isTransientNetworkError(cause) {
			return nil, &TransientDiscoveryError{Cause: err}
		}
		return nil, err
	}

	return cfg, nil
}
