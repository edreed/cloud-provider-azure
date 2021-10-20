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
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

var (
	errProcessFailed        = errors.New("process failed")
	defaultLogger           = NewStandardLogger(WithVerboseLogging())
	defaultProcessorOptions = []ProcessorOption{
		WithLogger(defaultLogger),
	}
	processorDoTestCases = []struct {
		description     string
		numValues       int
		valuesRate      time.Duration
		processDuration time.Duration
		timeout         time.Duration
		options         []ProcessorOption
		failWithErr     error
		expectedErr     error
		failEvery       int
	}{
		{
			description:     "[Success] Single entry",
			numValues:       1,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Success] Multiple entries & batches - race",
			numValues:       10,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Success] Multiple entries & batches - 20ms/value",
			numValues:       10,
			valuesRate:      20 * time.Millisecond,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Success] Multiple entries & batches - 20ms/value, delay 50ms before start",
			numValues:       10,
			valuesRate:      20 * time.Millisecond,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			options:         append(defaultProcessorOptions, WithDelayBeforeStart(50*time.Millisecond)),
		},
		{
			description:     "[Success] Multiple entries & batches - 20ms/value, batch rate limit of 1 QPS",
			numValues:       10,
			valuesRate:      20 * time.Millisecond,
			processDuration: 100 * time.Millisecond,
			timeout:         2 * time.Minute,
			options:         append(defaultProcessorOptions, WithBatchLimits(rate.Limit(1.0), 1)),
		},
		{
			description:     "[Success] Multiple entries & batches - 20ms/value, global rate limit of 5 QPS",
			numValues:       10,
			valuesRate:      20 * time.Millisecond,
			processDuration: 100 * time.Millisecond,
			timeout:         2 * time.Minute,
			options:         append(defaultProcessorOptions, WithGlobalLimits(rate.Limit(5.0), 1)),
		},
		{
			description:     "[Partial] Partial success with multiple entries & batches - race",
			numValues:       10,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			failEvery:       2,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Error single value",
			numValues:       1,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			failWithErr:     errProcessFailed,
			expectedErr:     errProcessFailed,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Error mutiple values - race",
			numValues:       10,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			failWithErr:     errProcessFailed,
			expectedErr:     errProcessFailed,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Error mutiple values - 20ms/value",
			numValues:       10,
			valuesRate:      20 * time.Millisecond,
			processDuration: 100 * time.Millisecond,
			timeout:         1 * time.Minute,
			failWithErr:     errProcessFailed,
			expectedErr:     errProcessFailed,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Timeout single value",
			numValues:       1,
			processDuration: 200 * time.Millisecond,
			timeout:         100 * time.Millisecond,
			expectedErr:     context.DeadlineExceeded,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Timeout mutiple values - race",
			numValues:       10,
			processDuration: 200 * time.Millisecond,
			timeout:         100 * time.Millisecond,
			expectedErr:     context.DeadlineExceeded,
			options:         defaultProcessorOptions,
		},
		{
			description:     "[Failure] Timeout mutiple values - 40ms/value",
			numValues:       10,
			valuesRate:      40 * time.Millisecond,
			processDuration: 200 * time.Millisecond,
			timeout:         100 * time.Millisecond,
			expectedErr:     context.DeadlineExceeded,
			options:         defaultProcessorOptions,
		},
	}
)

func getProcessingFailedErr(batch string, value int) error {
	return fmt.Errorf("processing failed for value %d of batch %q", value, batch)
}

func TestProcessorDo(t *testing.T) {
	for _, test := range processorDoTestCases {
		test := test
		t.Run(test.description, func(t *testing.T) {
			var wait sync.WaitGroup

			batchFn := func(ctx context.Context, key string, values []interface{}) (results []interface{}, err error) {
				t.Logf("Processing %d values for key %s: %v", len(values), key, values)

				select {
				case <-time.After(test.processDuration):
				case <-ctx.Done():
					t.Logf("Timeout processing %d values for key %s: %v", len(values), key, values)
					return nil, ctx.Err()
				}

				if test.failWithErr != nil {
					return nil, test.failWithErr
				}

				results = make([]interface{}, len(values))
				copy(results, values)

				if test.failEvery != 0 {
					for i, result := range results {
						value := result.(int)
						if ((value + 1) % test.failEvery) == 0 {
							results[i] = getProcessingFailedErr(key, value)
						}
					}
				}

				return
			}

			processor := NewProcessor(batchFn, test.options...)

			startTime := time.Now()

			for b := 0; b < 5; b++ {
				bucket := fmt.Sprintf("bucket%d", b)
				for i := 0; i < test.numValues; i++ {
					i := i
					wait.Add(1)
					go func() {
						defer wait.Done()
						if test.valuesRate != 0 {
							<-time.After(time.Until(startTime.Add(time.Duration(i * int(test.valuesRate)))))
						}
						ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
						defer cancel()
						result, err := processor.Do(ctx, bucket, i)
						if test.failEvery == 0 || ((i+1)%test.failEvery) != 0 {
							require.Equal(t, test.expectedErr, err)
						} else {
							require.Equal(t, getProcessingFailedErr(bucket, i), err)
						}
						if err == nil {
							require.Equal(t, i, result)
						}
					}()
				}
			}

			wait.Wait()

			for b := 0; b < 5; b++ {
				processor.Delete(fmt.Sprintf("bucket%d", b))
			}
		})
	}
}

func TestProcessorDoChan(t *testing.T) {
	for _, test := range processorDoTestCases {
		test := test
		t.Run(test.description, func(t *testing.T) {
			var wait sync.WaitGroup

			batchFn := func(ctx context.Context, key string, values []interface{}) (results []interface{}, err error) {
				t.Logf("Processing %d values for key %s: %v", len(values), key, values)

				select {
				case <-time.After(test.processDuration):
				case <-ctx.Done():
					t.Logf("Timeout processing %d values for key %s: %v", len(values), key, values)
					return nil, ctx.Err()
				}

				if test.failWithErr != nil {
					return nil, test.failWithErr
				}

				results = make([]interface{}, len(values))
				copy(results, values)

				if test.failEvery != 0 {
					for i, result := range results {
						value := result.(int)
						if ((value + 1) % test.failEvery) == 0 {
							results[i] = getProcessingFailedErr(key, value)
						}
					}
				}

				return
			}

			processor := NewProcessor(batchFn, test.options...)

			startTime := time.Now()

			for b := 0; b < 5; b++ {
				bucket := fmt.Sprintf("bucket%d", b)
				for i := 0; i < test.numValues; i++ {
					i := i
					wait.Add(1)
					go func() {
						defer wait.Done()
						if test.valuesRate != 0 {
							<-time.After(time.Until(startTime.Add(time.Duration(i * int(test.valuesRate)))))
						}
						ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
						defer cancel()
						resultChan := processor.DoChan(ctx, bucket, i)

						// This test explicitly does not select-wait on ctx.Done() as a well-formed program should.
						// This is done in order to catch cases where a batched item is dropped with no results.
						select {
						case <-time.After(maxDuration(test.timeout, time.Duration(2*test.numValues*int(test.processDuration)))):
							t.Errorf("Failed to process value %d for key %s", i, bucket)
						case result := <-resultChan:
							if test.failEvery == 0 || ((i+1)%test.failEvery) != 0 {
								require.Equal(t, test.expectedErr, result.err)
							} else {
								require.Equal(t, getProcessingFailedErr(bucket, i), result.err)
							}
							if result.err == nil {
								require.Equal(t, i, result.value)
							}
						}
					}()
				}
			}

			wait.Wait()

			for b := 0; b < 5; b++ {
				processor.Delete(fmt.Sprintf("bucket%d", b))
			}
		})
	}
}
