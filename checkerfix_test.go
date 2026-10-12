package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Typed-path extraction: full proposal body shape.
func TestExtractPaymentIDTyped(t *testing.T) {
	body := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-AAA","extensibilityDisplayName":"Shopify Payments"}}]}}}}}}}`)
	id, gw := extractPaymentID(body)
	if id != "gid-AAA" || gw != "Shopify Payments" {
		t.Errorf("got (%q,%q), want (gid-AAA,Shopify Payments)", id, gw)
	}
}

// Fallback path: fragment without the typed envelope (e.g. seller subtree).
func TestExtractPaymentIDFallback(t *testing.T) {
	body := []byte(`{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-BBB","name":"Stripe"}}]}}}`)
	id, gw := extractPaymentID(body)
	if id != "gid-BBB" || gw != "Stripe" {
		t.Errorf("got (%q,%q), want (gid-BBB,Stripe)", id, gw)
	}
}

func TestExtractPaymentIDEmpty(t *testing.T) {
	if id, _ := extractPaymentID([]byte(`{"data":{}}`)); id != "" {
		t.Errorf("got %q, want empty", id)
	}
}

// Write-through must land on the exact nested submit path and never panic.
func TestSetSubmitPaymentID(t *testing.T) {
	sv := map[string]interface{}{
		"input": map[string]interface{}{
			"payment": map[string]interface{}{
				"paymentLines": []map[string]interface{}{{
					"paymentMethod": map[string]interface{}{
						"directPaymentMethod": map[string]interface{}{
							"paymentMethodIdentifier": "gid-OLD",
						},
					},
				}},
			},
		},
	}
	setSubmitPaymentID(sv, "gid-NEW")
	got := sv["input"].(map[string]interface{})["payment"].(map[string]interface{})["paymentLines"].([]map[string]interface{})[0]["paymentMethod"].(map[string]interface{})["directPaymentMethod"].(map[string]interface{})["paymentMethodIdentifier"]
	if got != "gid-NEW" {
		t.Errorf("got %q, want gid-NEW", got)
	}
	// Bad shapes: no-op, no panic.
	setSubmitPaymentID(map[string]interface{}{}, "x")
	setSubmitPaymentID(map[string]interface{}{"input": 1}, "x")
}

// Currency sync must align buyerIdentity and concrete taxes, and skip
// any:true proposal taxes.
func TestSyncProposalCurrency(t *testing.T) {
	v := map[string]interface{}{
		"buyerIdentity": map[string]interface{}{
			"customer": map[string]string{"presentmentCurrency": "USD", "countryCode": "US"},
		},
		"taxes": map[string]interface{}{
			"proposedTotalAmount": map[string]interface{}{"value": map[string]string{"amount": "1", "currencyCode": "USD"}},
		},
	}
	syncProposalCurrency(v, "GBP")
	if c := v["buyerIdentity"].(map[string]interface{})["customer"].(map[string]string)["presentmentCurrency"]; c != "GBP" {
		t.Errorf("buyerIdentity = %q, want GBP", c)
	}
	if c := v["taxes"].(map[string]interface{})["proposedTotalAmount"].(map[string]interface{})["value"].(map[string]string)["currencyCode"]; c != "GBP" {
		t.Errorf("taxes = %q, want GBP", c)
	}
	any := map[string]interface{}{
		"buyerIdentity": map[string]interface{}{
			"customer": map[string]string{"presentmentCurrency": "USD"},
		},
		"taxes": map[string]interface{}{"proposedTotalAmount": map[string]interface{}{"any": true}},
	}
	syncProposalCurrency(any, "GBP") // must not panic
	if c := any["buyerIdentity"].(map[string]interface{})["customer"].(map[string]string)["presentmentCurrency"]; c != "GBP" {
		t.Errorf("buyerIdentity = %q, want GBP", c)
	}
}

// Proposal query must expose the seller's payment flexibility template
// (read-only visibility for FLEXIBILITY_MISMATCH tracing).
func TestProposalQueryFlexVisibility(t *testing.T) {
	if !strings.Contains(QUERY_PROPOSAL, "paymentFlexibilityPaymentTermsTemplate{") {
		t.Error("QUERY_PROPOSAL missing paymentFlexibilityPaymentTermsTemplate selection")
	}
	if !strings.Contains(QUERY_PROPOSAL, "depositConfiguration{") {
		t.Error("QUERY_PROPOSAL missing depositConfiguration selection")
	}
}

// Session-token write-through must land on input.queueToken/checkpointData
// and never panic on bad shapes.
func TestSetSubmitSessionTokens(t *testing.T) {
	sv := map[string]interface{}{
		"input": map[string]interface{}{
			"queueToken": "old-q", "checkpointData": "old-c",
		},
	}
	setSubmitSessionTokens(sv, "new-q", "new-c")
	_in := sv["input"].(map[string]interface{})
	if _in["queueToken"] != "new-q" || _in["checkpointData"] != "new-c" {
		t.Errorf("got %q/%q, want new-q/new-c", _in["queueToken"], _in["checkpointData"])
	}
	setSubmitSessionTokens(map[string]interface{}{}, "q", "c")
	setSubmitSessionTokens(map[string]interface{}{"input": 1}, "q", "c")
}

func TestProxyConnectAuth(t *testing.T) {
	u, _ := url.Parse("http://user:p%40ss@host:8080")
	if got := proxyConnectAuth(u); got != "Basic dXNlcjpwQHNz" {
		t.Errorf("got %q", got)
	}
	u2, _ := url.Parse("http://host:8080")
	if got := proxyConnectAuth(u2); got != "" {
		t.Errorf("no-creds proxy: got %q", got)
	}
	if got := proxyConnectAuth(nil); got != "" {
		t.Errorf("nil url: got %q", got)
	}
}

func TestReadConnectResponse(t *testing.T) {
	c1, c2 := net.Pipe()
	go func() {
		fmt.Fprintf(c2, "HTTP/1.1 200 Connection Established\r\n\r\n")
		c2.Close()
	}()
	if err := readConnectResponse(c1, 1024); err != nil {
		t.Errorf("200: %v", err)
	}
	c1.Close()

	d1, d2 := net.Pipe()
	go func() {
		fmt.Fprintf(d2, "HTTP/1.1 403 Forbidden\r\n\r\n")
		d2.Close()
	}()
	if err := readConnectResponse(d1, 1024); err == nil {
		t.Error("403: want error, got nil")
	}
	d1.Close()
}

// Local CONNECT stub: asserts request shape, answers 200, then echoes.
func TestConnectTunnel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	defer ln.Close()
	gotReq := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var hdr []byte
		one := make([]byte, 1)
		for !strings.HasSuffix(string(hdr), "\r\n\r\n") && len(hdr) < 4096 {
			n, err := c.Read(one)
			if err != nil {
				return
			}
			hdr = append(hdr, one[:n]...)
		}
		gotReq <- string(hdr)
		c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		io.Copy(c, c)
	}()
	ctx := context.Background()
	tunnel, err := connectTunnel(ctx, ln.Addr().String(), "target.example:443", "Basic dXNlcjpwYXNz")
	if err != nil {
		t.Fatalf("tunnel: %v", err)
	}
	defer tunnel.Close()
	req := <-gotReq
	if !strings.HasPrefix(req, "CONNECT target.example:443 HTTP/1.1\r\n") {
		t.Errorf("bad request line: %q", req)
	}
	if !strings.Contains(req, "Proxy-Authorization: Basic dXNlcjpwYXNz\r\n") {
		t.Errorf("missing auth: %q", req)
	}
	tunnel.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(tunnel, "ping")
	echo := make([]byte, 4)
	if _, err := io.ReadFull(tunnel, echo); err != nil || string(echo) != "ping" {
		t.Errorf("echo: %q %v", echo, err)
	}
}

// utls Chrome handshake against a local TLS server (handshake validity,
// not wall behavior — that only a live canary proves).
func TestChromeHandshakeLocal(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	raw, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Skip("no loopback")
	}
	conn, err := chromeHandshake(context.Background(), raw, u.Hostname())
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	conn.Close()
}

// H1 gate flips the direct transport; stock path stays shared + H2.
func TestH1GateDirect(t *testing.T) {
	t.Setenv("SHOPAPI_H1_ONLY", "1")
	t.Setenv("TLS_IMPERSONATE", "")
	c, err := getProxyClient("")
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatal("not *http.Transport")
	}
	if tr.ForceAttemptHTTP2 {
		t.Error("H1 gate on but H2 forced")
	}
	if tr == GlobalClient.Transport {
		t.Error("gated client must not reuse the shared transport")
	}
}

// Impersonation arms DialTLSContext and drops Transport.Proxy (no double dial).
func TestImpersonatedClientBuilds(t *testing.T) {
	t.Setenv("TLS_IMPERSONATE", "chrome")
	t.Setenv("SHOPAPI_H1_ONLY", "")
	for _, p := range []string{"", "http://u:p@127.0.0.1:9", "socks5://u:p@127.0.0.1:1080"} {
		c, err := getProxyClient(p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		tr := c.Transport.(*http.Transport)
		if tr.DialTLSContext == nil {
			t.Errorf("%q: DialTLSContext not armed", p)
		}
		if tr.Proxy != nil {
			t.Errorf("%q: Transport.Proxy must be nil under impersonation", p)
		}
	}
}

// ADDRESS2 remedy echoes address1 into the empty second line, both maps.
func TestEnsureAddress2(t *testing.T) {
	ba := map[string]interface{}{
		"streetAddress": map[string]string{"address1": "Main 1", "city": "X"},
	}
	ss := map[string]string{"address1": "Main 1", "city": "X"}
	ensureAddress2(ba, ss)
	if got := ba["streetAddress"].(map[string]string)["address2"]; got != "Main 1" {
		t.Errorf("billing address2 = %q", got)
	}
	if got := ss["address2"]; got != "Main 1" {
		t.Errorf("ship address2 = %q", got)
	}
	// Present values are never clobbered.
	ba["streetAddress"].(map[string]string)["address2"] = "Apt 5"
	ss["address2"] = "Apt 5"
	ensureAddress2(ba, ss)
	if got := ba["streetAddress"].(map[string]string)["address2"]; got != "Apt 5" {
		t.Errorf("billing clobbered: %q", got)
	}
	// Bad shapes: no-op, no panic.
	ensureAddress2(map[string]interface{}{}, ss)
	ensureAddress2(map[string]interface{}{"streetAddress": 1}, ss)
}

// Pinned-variant lookup: exact hit, case-insensitive fallback, miss.
func TestPinnedVariant(t *testing.T) {
	if n := len(pinnedVariants); n != 315 {
		t.Fatalf("pinnedVariants = %d, want 315", n)
	}
	if got := pinnedVariant("https://arizonasun.myshopify.com"); got != "44042332176564" {
		t.Errorf("arizonasun pin = %q", got)
	}
	if got := pinnedVariant("https://ARIZONASUN.myshopify.com"); got != "44042332176564" {
		t.Errorf("case-insensitive pin = %q", got)
	}
	if got := pinnedVariant("https://no-such-store.example.com"); got != "" {
		t.Errorf("unknown store pin = %q, want empty", got)
	}
}

// sellerCurrencyFrom reads checkoutTotal.currencyCode from proposal or
// submit-rejection bodies; empty when the seller published none.
func TestSellerCurrencyFrom(t *testing.T) {
	prop := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"checkoutTotal":{"value":{"amount":"12.00","currencyCode":"EUR"}}}}}}}}`)
	if got := sellerCurrencyFrom(prop); got != "EUR" {
		t.Errorf("proposal: got %q, want EUR", got)
	}
	rej := []byte(`{"data":{"submitForCompletion":{"sellerProposal":{"checkoutTotal":{"value":{"amount":"5.00","currencyCode":"gbp"}}}}}}`)
	if got := sellerCurrencyFrom(rej); got != "GBP" {
		t.Errorf("rejection: got %q, want GBP", got)
	}
	if got := sellerCurrencyFrom([]byte(`{"data":{}}`)); got != "" {
		t.Errorf("absent: got %q, want empty", got)
	}
}

// receiptURL prefers the order-status page, then redirect, confirmation,
// and post-purchase URLs; nulls and empties never win.
func TestReceiptTransient(t *testing.T) {
	if !receiptTransient("https://x/checkouts/sessions/checkout-completion/redirect?_t=abc") {
		t.Error("sessions handoff must flag transient")
	}
	if receiptTransient("https://store.com/checkouts/order-status-page/xyz") {
		t.Error("order page must not flag transient")
	}
}

func TestReceiptURL(t *testing.T) {
	full := []byte(`{"data":{"submitForCompletion":{"receipt":{"__typename":"ProcessedReceipt","orderStatusPageUrl":"https://s/o1","redirectUrl":"https://s/r1","confirmationPage":{"url":"https://s/c1"}}}}}`)
	if got, src := receiptURL(full); got != "https://s/o1" || src != "orderStatusPageUrl" {
		t.Errorf("prefer status page: %q", got)
	}
	redirOnly := []byte(`{"data":{"receipt":{"__typename":"ProcessedReceipt","orderStatusPageUrl":null,"redirectUrl":"https://s/r2"}}}`)
	if got, src := receiptURL(redirOnly); got != "https://s/r2" || src != "redirectUrl" {
		t.Errorf("redirect fallback: %q", got)
	}
	confirmOnly := []byte(`{"data":{"receipt":{"__typename":"ProcessedReceipt","orderStatusPageUrl":null,"redirectUrl":null,"confirmationPage":{"url":"https://s/c3"}}}}}`)
	if got, src := receiptURL(confirmOnly); got != "https://s/c3" || src != "confirmationPage" {
		t.Errorf("confirmation fallback: %q", got)
	}
	empty := []byte(`{"data":{"receipt":{"__typename":"ProcessedReceipt","orderStatusPageUrl":null,"redirectUrl":null}}}`)
	if got, src := receiptURL(empty); got != "" || src != "none" {
		t.Errorf("both null: got %q, want empty", got)
	}
}

// Preference: shopify_payments line wins even when sorted second.
func TestExtractPaymentIDPrefersShopifyPayments(t *testing.T) {
	body := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-RAZOR","name":"razorpay"}},{"paymentMethod":{"paymentMethodIdentifier":"gid-SP","name":"shopify_payments","extensibilityDisplayName":"Shopify Payments"}}]}}}}}}}`)
	id, _ := extractPaymentID(body)
	if id != "gid-SP" {
		t.Errorf("got %q, want gid-SP", id)
	}
}

// Classifier: present / confirmed-absent / unknown / empty.
func TestShopifyPaymentsPresent(t *testing.T) {
	sp := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-SP","name":"shopify_payments"}}]}}}}}}}`)
	if hasSP, absent, _ := shopifyPaymentsPresent(sp); !hasSP || absent {
		t.Errorf("present case: hasSP=%v absent=%v", hasSP, absent)
	}
	offsite := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-RZ","name":"razorpay"}}]}}}}}}}`)
	if hasSP, absent, _ := shopifyPaymentsPresent(offsite); hasSP || !absent {
		t.Errorf("absent case: hasSP=%v absent=%v", hasSP, absent)
	}
	nameless := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-DIRECT"}}]}}}}}}}`)
	if hasSP, absent, _ := shopifyPaymentsPresent(nameless); hasSP || absent {
		t.Errorf("unknown case must proceed, never skip: hasSP=%v absent=%v", hasSP, absent)
	}
	mixed := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-RZ","name":"razorpay"}},{"paymentMethod":{"paymentMethodIdentifier":"gid-DIRECT"}}]}}}}}}}`)
	if hasSP, absent, _ := shopifyPaymentsPresent(mixed); hasSP || absent {
		t.Errorf("mixed named+nameless must proceed: hasSP=%v absent=%v", hasSP, absent)
	}
	empty := []byte(`{"data":{}}`)
	if hasSP, absent, _ := shopifyPaymentsPresent(empty); hasSP || absent {
		t.Errorf("empty case: hasSP=%v absent=%v", hasSP, absent)
	}
}

// needsReproposal fires on flexibility rotation and bare payment-method
// mismatch — both never-converging session states — and nothing else.
func TestNeedsReproposal(t *testing.T) {
	for _, c := range []string{"PAYMENTS_PAYMENT_FLEXIBILITY_TERMS_ID_MISMATCH", "PAYMENTS_METHOD"} {
		if !needsReproposal(c) {
			t.Errorf("%q should trigger re-proposal", c)
		}
	}
	for _, c := range []string{"TAX_NEW_TAX_MUST_BE_ACCEPTED", "WAITING_PENDING_TERMS", "DELIVERY_NO_DELIVERY_STRATEGY_AVAILABLE", "PAYMENTS_CREDIT_CARD_BRAND_NOT_SUPPORTED", ""} {
		if needsReproposal(c) {
			t.Errorf("%q must NOT trigger re-proposal", c)
		}
	}
}

func TestCustomShipValid(t *testing.T) {
	ok := CustomShip{Name: "Aarav Sharma", Email: "aarav.test@gmail.com", Phone: "9876543210", Addr1: "14 MG Road", Addr2: "Apt 3B", City: "Bengaluru", State: "KA", Pin: "560001"}
	if !ok.valid() {
		t.Error("valid IN profile rejected")
	}
	badPhone := ok
	badPhone.Phone = "12345"
	if badPhone.valid() {
		t.Error("short phone accepted")
	}
	badMail := ok
	badMail.Email = "a@yahoo.com"
	if badMail.valid() {
		t.Error("non-gmail accepted")
	}
	empty := CustomShip{}
	if empty.valid() {
		t.Error("empty profile accepted")
	}
}
