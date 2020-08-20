package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
)

func TestPackage(t *testing.T) { TestingT(t) }

type TssHttpServerTestSuite struct {
}

var _ = Suite(&TssHttpServerTestSuite{})

func (TssHttpServerTestSuite) TestNewTssHttpServer(c *C) {
	tssServer := &MockTssServer{}
	s := NewTssHttpServer("127.0.0.1:8080", tssServer)
	c.Assert(s, NotNil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.Start()
		c.Assert(err, IsNil)
	}()
	time.Sleep(time.Second)
	c.Assert(s.Stop(), IsNil)
	tssServer.failToStart = true
	c.Assert(s.Start(), NotNil)
}

func (TssHttpServerTestSuite) TestPingHandler(c *C) {
	tssServer := &MockTssServer{}
	s := NewTssHttpServer("127.0.0.1:8080", tssServer)
	c.Assert(s, NotNil)
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	res := httptest.NewRecorder()
	s.pingHandler(res, req)
	c.Assert(res.Code, Equals, http.StatusOK)
}

func (TssHttpServerTestSuite) TestGetP2pIDHandler(c *C) {
	tssServer := &MockTssServer{}
	s := NewTssHttpServer("127.0.0.1:8080", tssServer)
	c.Assert(s, NotNil)
	req := httptest.NewRequest(http.MethodGet, "/p2pid", nil)
	res := httptest.NewRecorder()
	s.getP2pIDHandler(res, req)
	c.Assert(res.Code, Equals, http.StatusOK)
}

func (TssHttpServerTestSuite) TestGetNodeStatusHandler(c *C) {
	tssServer := &MockTssServer{}
	s := NewTssHttpServer("127.0.0.1:8080", tssServer)
	c.Assert(s, NotNil)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	res := httptest.NewRecorder()
	s.getNodeStatusHandler(res, req)
	c.Assert(res.Code, Equals, http.StatusOK)
	var status common.TssStatus
	c.Assert(json.Unmarshal(res.Body.Bytes(), &status), IsNil)
}

func (TssHttpServerTestSuite) TestKeygenHandler(c *C) {
	normalKeygenRequest := `{"keys":["thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"]}`
	testCases := []struct {
		name          string
		reqProvider   func() *http.Request
		setter        func(s *MockTssServer)
		resultChecker func(c *C, w *httptest.ResponseRecorder)
	}{
		{
			name: "method get should return status method not allowed",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/keygen", nil)
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusMethodNotAllowed)
			},
		},
		{
			name: "nil request body should return status bad request",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keygen", nil)
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusBadRequest)
			},
		},
		{
			name: "fail to keygen should return status internal server error",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keygen",
					bytes.NewBufferString(normalKeygenRequest))
			},
			setter: func(s *MockTssServer) {
				s.failToKeyGen = true
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusOK)
			},
		},
		{
			name: "normal",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keygen",
					bytes.NewBufferString(normalKeygenRequest))
			},

			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusOK)
				var resp keygen.Response
				c.Assert(json.Unmarshal(w.Body.Bytes(), &resp), IsNil)
			},
		},
	}
	for _, tc := range testCases {
		c.Log(tc.name)
		tssServer := &MockTssServer{}
		s := NewTssHttpServer("127.0.0.1:8080", tssServer)
		c.Assert(s, NotNil)
		if tc.setter != nil {
			tc.setter(tssServer)
		}
		req := tc.reqProvider()
		res := httptest.NewRecorder()
		s.keygenHandler(res, req)
		tc.resultChecker(c, res)
	}
}

func (TssHttpServerTestSuite) TestKeysignHandler(c *C) {
	var normalKeySignRequest string = `{
    "pool_pub_key": "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
    "message": "helloworld",
    "signer_pub_keys": [
        "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
        "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09",
        "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69",
        "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"
    ]
}`
	testCases := []struct {
		name          string
		reqProvider   func() *http.Request
		setter        func(s *MockTssServer)
		resultChecker func(c *C, w *httptest.ResponseRecorder)
	}{
		{
			name: "method get should return status method not allowed",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/keysign", nil)
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusMethodNotAllowed)
			},
		},
		{
			name: "nil request body should return status bad request",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keysign", nil)
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusBadRequest)
			},
		},
		{
			name: "fail to keygen should return status internal server error",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keysign",
					bytes.NewBufferString(normalKeySignRequest))
			},
			setter: func(s *MockTssServer) {
				s.failToKeySign = true
			},
			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusInternalServerError)
			},
		},
		{
			name: "normal",
			reqProvider: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/keysign",
					bytes.NewBufferString(normalKeySignRequest))
			},

			resultChecker: func(c *C, w *httptest.ResponseRecorder) {
				c.Assert(w.Code, Equals, http.StatusOK)
				var resp keygen.Response
				c.Assert(json.Unmarshal(w.Body.Bytes(), &resp), IsNil)
			},
		},
	}
	for _, tc := range testCases {
		c.Log(tc.name)
		tssServer := &MockTssServer{}
		s := NewTssHttpServer("127.0.0.1:8080", tssServer)
		c.Assert(s, NotNil)
		if tc.setter != nil {
			tc.setter(tssServer)
		}
		req := tc.reqProvider()
		res := httptest.NewRecorder()
		s.keySignHandler(res, req)
		tc.resultChecker(c, res)
	}
}
