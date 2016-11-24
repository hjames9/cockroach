// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Author: Tamir Duberstein (tamird@gmail.com)

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/github"
)

func TestRunGH(t *testing.T) {
	const (
		expOwner      = "cockroachdb"
		expRepo       = "cockroach"
		envPkg        = "foo/bar/baz"
		envPropEvalKV = "true"
		envTags       = "deadlock"
		envGoFlags    = "race"
		sha           = "abcd123"
		serverURL     = "https://teamcity.example.com"
		buildID       = 8008135
		issueID       = 1337
	)

	for key, value := range map[string]string{
		teamcityVCSNumberEnv: sha,
		teamcityServerURLEnv: serverURL,
		teamcityBuildIDEnv:   strconv.Itoa(buildID),

		pkgEnv:        envPkg,
		propEvalKVEnv: envPropEvalKV,
		tagsEnv:       envTags,
		goFlagsEnv:    envGoFlags,
	} {
		if val, ok := os.LookupEnv(key); ok {
			defer func() {
				if err := os.Setenv(key, val); err != nil {
					t.Error(err)
				}
			}()
		} else {
			defer func() {
				if err := os.Unsetenv(key); err != nil {
					t.Error(err)
				}
			}()
		}

		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
	}

	parameters := "```\n" + strings.Join([]string{
		propEvalKVEnv + "=" + envPropEvalKV,
		tagsEnv + "=" + envTags,
		goFlagsEnv + "=" + envGoFlags,
	}, "\n") + "\n```"

	for fileName, expectations := range map[string]struct {
		packageName string
		testName    string
		body        string
	}{
		"stress-failure": {
			packageName: envPkg,
			testName:    "TestReplicateQueueRebalance",
			body: "	<autogenerated>:12: storage/replicate_queue_test.go:103, condition failed to evaluate within 45s: not balanced: [10 1 10 1 8]",
		},
		"stress-fatal": {
			packageName: envPkg,
			testName:    "TestRaftRemoveRace",
			body:        "F161007 00:27:33.243126 449 storage/store.go:2446  [s3] [n3,s3,r1:/M{in-ax}]: could not remove placeholder after preemptive snapshot",
		},
	} {
		t.Run(fileName, func(t *testing.T) {
			file, err := os.Open(filepath.Join("testdata", fileName))
			if err != nil {
				t.Fatal(err)
			}

			issueBodyRe, err := regexp.Compile(
				fmt.Sprintf(`(?s)\ASHA: https://github.com/cockroachdb/cockroach/commits/%s

Parameters:
%s

Stress build found a failed test: %s

.*
%s
`,
					regexp.QuoteMeta(sha),
					regexp.QuoteMeta(parameters),
					regexp.QuoteMeta(fmt.Sprintf("%s/viewLog.html?buildId=%d&tab=buildLog", serverURL, buildID)),
					regexp.QuoteMeta(expectations.body),
				),
			)
			if err != nil {
				t.Fatal(err)
			}

			count := 0
			if err := runGH(
				file,
				func(owner string, repo string, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
					count++
					if owner != expOwner {
						t.Fatalf("got %s, expected %s", owner, expOwner)
					}
					if repo != expRepo {
						t.Fatalf("got %s, expected %s", repo, expRepo)
					}
					if expected := fmt.Sprintf("%s: %s failed under stress", expectations.packageName, expectations.testName); *issue.Title != expected {
						t.Fatalf("got %s, expected %s", *issue.Title, expected)
					}
					if !issueBodyRe.MatchString(*issue.Body) {
						t.Fatalf("got:\n%s\nexpected:\n%s", *issue.Body, issueBodyRe)
					}
					if length := len(*issue.Body); length > githubIssueBodyMaximumLength {
						t.Fatalf("issue length %d exceeds (undocumented) maximum %d", length, githubIssueBodyMaximumLength)
					}
					return &github.Issue{ID: github.Int(issueID)}, nil, nil
				},
			); err != nil {
				t.Fatal(err)
			}
			if expected := 1; count != expected {
				t.Fatalf("%d issues were posted, expected %d", count, expected)
			}
		})
	}
}
