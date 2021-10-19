/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package batch

import (
	"bytes"
	"fmt"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
)

var tests = []struct {
	description    string
	loggingFn      func(logger Logger, message string)
	message        string
	expectedPrefix string
}{
	{
		description:    "Verbose logging test",
		loggingFn:      func(logger Logger, message string) { logger.Verbosef(message) },
		message:        "This is a verbose log.",
		expectedPrefix: "VERBOSE: ",
	},
	{
		description:    "Informational logging test",
		loggingFn:      func(logger Logger, message string) { logger.Infof(message) },
		message:        "This is an informational log.",
		expectedPrefix: "INFO: ",
	},
	{
		description:    "Warning logging test",
		loggingFn:      func(logger Logger, message string) { logger.Warningf(message) },
		message:        "This is a warning log.",
		expectedPrefix: "WARN: ",
	},
	{
		description:    "Error logging test",
		loggingFn:      func(logger Logger, message string) { logger.Errorf(message) },
		message:        "This is an error log.",
		expectedPrefix: "ERROR: ",
	},
}

func TestNewStandardLogger(t *testing.T) {
	for _, test := range tests {
		test := test
		t.Run(test.description, func(t *testing.T) {
			buffer := &bytes.Buffer{}
			logger := NewStandardLogger(WithOutput(buffer, "", log.LstdFlags), WithVerboseLogging())
			test.loggingFn(logger, test.message)
			expectedLog := test.expectedPrefix + test.message
			assert.Contains(t, buffer.String(), expectedLog)
		})
	}
}

func TestNewLoggerAdapter(t *testing.T) {
	for _, test := range tests {
		test := test
		t.Run(test.description, func(t *testing.T) {
			buffer := &bytes.Buffer{}
			logger := NewLoggerAdapter(
				WithVerboseLogger(func(format string, v ...interface{}) { buffer.WriteString(fmt.Sprintf("VERBOSE: "+format, v...)) }),
				WithInfoLogger(func(format string, v ...interface{}) { buffer.WriteString(fmt.Sprintf("INFO: "+format, v...)) }),
				WithWarningLogger(func(format string, v ...interface{}) { buffer.WriteString(fmt.Sprintf("WARN: "+format, v...)) }),
				WithErrorLogger(func(format string, v ...interface{}) { buffer.WriteString(fmt.Sprintf("ERROR: "+format, v...)) }),
			)
			test.loggingFn(logger, test.message)
			expectedLog := test.expectedPrefix + test.message
			assert.Contains(t, buffer.String(), expectedLog)
		})
	}
}
