// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package client_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type clientSuite struct {
	cli    *client.Client
	req    *http.Request
	rsp    string
	err    error
	header http.Header
	status int
}

var _ = check.Suite(&clientSuite{})

func (cs *clientSuite) SetUpTest(c *check.C) {
	cs.cli = client.New()
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.rsp = ""
	cs.req = nil
	cs.header = nil
	cs.status = http.StatusOK

	dirs.SetRootDir(c.MkDir())
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	rsp := &http.Response{
		Body:       ioutil.NopCloser(strings.NewReader(cs.rsp)),
		Header:     cs.header,
		StatusCode: cs.status,
	}
	return rsp, cs.err
}

func (cs *clientSuite) TestClientDoReportsErrors(c *check.C) {
	cs.err = errors.New("ouchie")
	err := cs.cli.Do("GET", "/", nil, nil, nil)
	c.Check(err, check.Equals, cs.err)
}

func (cs *clientSuite) TestClientWorks(c *check.C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := ioutil.NopCloser(strings.NewReader(""))
	err := cs.cli.Do("GET", "/this", nil, reqBody, &v)
	c.Check(err, check.IsNil)
	c.Check(v, check.DeepEquals, []int{1, 2})
	c.Assert(cs.req, check.NotNil)
	c.Assert(cs.req.URL, check.NotNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.Body, check.Equals, reqBody)
	c.Check(cs.req.URL.Path, check.Equals, "/this")
}

func (cs *clientSuite) TestClientSysInfo(c *check.C) {
	cs.rsp = `{"type": "sync", "result":
                     {"flavor": "f",
                      "release": "r",
                      "default_channel": "dc",
                      "api_compat": "42",
                      "store": "store"}}`
	sysInfo, err := cs.cli.SysInfo()
	c.Check(err, check.IsNil)
	c.Check(sysInfo, check.DeepEquals, &client.SysInfo{
		Flavor:           "f",
		Release:          "r",
		DefaultChannel:   "dc",
		APICompatibility: "42",
		Store:            "store",
	})
}

func (cs *clientSuite) TestClientIntegration(c *check.C) {
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0755), check.IsNil)
	l, err := net.Listen("unix", dirs.SnapdSocket)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", dirs.SnapdSocket, err)
	}

	f := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/2.0/system-info")
		c.Check(r.URL.RawQuery, check.Equals, "")

		fmt.Fprintln(w, `{"type":"sync", "result":{"store":"X"}}`)
	}

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: http.HandlerFunc(f)},
	}
	srv.Start()
	defer srv.Close()

	cli := client.New()
	si, err := cli.SysInfo()
	c.Check(err, check.IsNil)
	c.Check(si.Store, check.Equals, "X")
}

func (cs *clientSuite) TestClientReportsOpError(c *check.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `.*server error: "potatoes"`)
}

func (cs *clientSuite) TestClientReportsOpErrorStr(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status_code": 400,
		"type": "error"
	}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `.*server error: "Bad Request"`)
}

func (cs *clientSuite) TestClientReportsBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `.*expected sync response, got "what"`)
}

func (cs *clientSuite) TestClientReportsOuterJSONError(c *check.C) {
	cs.rsp = "this isn't really json is it"
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `.*invalid character .*`)
}

func (cs *clientSuite) TestClientReportsInnerJSONError(c *check.C) {
	cs.rsp = `{"type": "sync", "result": "this isn't really json is it"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `.*failed to unmarshal.*`)
}

func (cs *clientSuite) TestClientCapabilities(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"capabilities": {
				"n": {
					"name": "n",
					"label": "l",
					"type": "t",
					"attrs": {"k": "v"}
				}
			}
		}
	}`
	caps, err := cs.cli.Capabilities()
	c.Check(err, check.IsNil)
	c.Check(caps, check.DeepEquals, map[string]client.Capability{
		"n": client.Capability{
			Name:  "n",
			Label: "l",
			Type:  "t",
			Attrs: map[string]string{"k": "v"},
		},
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/capabilities")
}

func (cs *clientSuite) TestClientAddCapability(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
		}
	}`
	cap := &client.Capability{
		Name:  "n",
		Label: "l",
		Type:  "t",
		Attrs: map[string]string{"k": "v"},
	}
	err := cs.cli.AddCapability(cap)
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"name":  "n",
		"label": "l",
		"type":  "t",
		"attrs": map[string]interface{}{
			"k": "v",
		},
	})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/capabilities")
}

func (cs *clientSuite) TestClientRemoveCapabilityOk(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.RemoveCapability("n")
	c.Check(err, check.IsNil)
	c.Check(cs.req.Body, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "DELETE")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/capabilities/n")
}

func (cs *clientSuite) TestClientRemoveCapabilityNotFound(c *check.C) {
	cs.rsp = `{
		"status": "Not Found",
		"status_code": 404,
		"type": "error",
		"result": {
			"message": "can't remove capability \"n\", no such capability"
		}
	}`
	err := cs.cli.RemoveCapability("n")
	c.Check(err, check.ErrorMatches, `.*can't remove capability \"n\", no such capability`)
	c.Check(cs.req.Body, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "DELETE")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/capabilities/n")
}

func (cs *clientSuite) TestParseError(c *check.C) {
	resp := &http.Response{
		Status: "404 Not Found",
	}
	err := client.ParseErrorInTest(resp)
	c.Check(err, check.ErrorMatches, `server error: "404 Not Found"`)

	h := http.Header{}
	h.Add("Content-Type", "application/json")
	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body: ioutil.NopCloser(strings.NewReader(`{
			"status_code": 400,
			"type": "error",
			"result": {
				"message": "invalid"
			}
		}`)),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, check.ErrorMatches, "invalid")

	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body:   ioutil.NopCloser(strings.NewReader("{}")),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, check.ErrorMatches, `server error: "400 Bad Request"`)
}