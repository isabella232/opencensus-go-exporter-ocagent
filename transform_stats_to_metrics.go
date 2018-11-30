// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ocagent

import (
	"errors"
	"time"

	"go.opencensus.io/exemplar"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"github.com/golang/protobuf/ptypes/timestamp"

	metricspb "github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1"
)

var (
	errNilMeasure  = errors.New("expecting a non-nil stats.Measure")
	errNilView     = errors.New("expecting a non-nil view.View")
	errNilViewData = errors.New("expecting a non-nil view.Data")
)

func viewDataToMetrics(vd *view.Data) (*metricspb.Metric, error) {
	if vd == nil {
		return nil, errNilViewData
	}

	descriptor, err := viewToMetricDescriptor(vd.View)
	if err != nil {
		return nil, err
	}

	timeseries, err := viewDataToTimeseries(vd)
	if err != nil {
		return nil, err
	}
	metric := &metricspb.Metric{
		Descriptor_: descriptor,
		Timeseries:  timeseries,
		// TODO: (@odeke-em) figure out how to derive
		// the Resource from the view or from environment?
		// Resource: derivedResource,
	}
	return metric, nil
}

func viewToMetricDescriptor(v *view.View) (*metricspb.Metric_MetricDescriptor, error) {
	if v == nil {
		return nil, errNilView
	}
	if v.Measure == nil {
		return nil, errNilMeasure
	}

	desc := &metricspb.Metric_MetricDescriptor{
		MetricDescriptor: &metricspb.MetricDescriptor{
			Name:        stringOrCall(v.Name, v.Measure.Name),
			Description: stringOrCall(v.Description, v.Measure.Description),
			Unit:        v.Measure.Unit(),
			Type:        aggregationToMetricDescriptorType(v),
			LabelKeys:   tagKeysToLabelKeys(v.TagKeys),
		},
	}
	return desc, nil
}

func stringOrCall(first string, call func() string) string {
	if first != "" {
		return first
	}
	return call()
}

func nameToMetricName(name string) *metricspb.Metric_Name {
	if name == "" {
		return nil
	}
	return &metricspb.Metric_Name{Name: name}
}

type measureType uint

const (
	measureUnknown measureType = iota
	measureInt64
	measureFloat64
)

func measureTypeFromMeasure(m stats.Measure) measureType {
	switch m.(type) {
	default:
		return measureUnknown
	case *stats.Float64Measure:
		return measureFloat64
	case *stats.Int64Measure:
		return measureInt64
	}
}

func aggregationToMetricDescriptorType(v *view.View) metricspb.MetricDescriptor_Type {
	if v == nil || v.Aggregation == nil {
		return metricspb.MetricDescriptor_UNSPECIFIED
	}
	if v.Measure == nil {
		return metricspb.MetricDescriptor_UNSPECIFIED
	}

	switch v.Aggregation.Type {
	default:
		return metricspb.MetricDescriptor_UNSPECIFIED

	case view.AggTypeCount:
		// Cumulative on int64
		return metricspb.MetricDescriptor_CUMULATIVE_INT64

	case view.AggTypeDistribution:
		// Cumulative types
		return metricspb.MetricDescriptor_CUMULATIVE_DISTRIBUTION

	case view.AggTypeLastValue:
		// Gauge types
		switch measureTypeFromMeasure(v.Measure) {
		case measureFloat64:
			return metricspb.MetricDescriptor_GAUGE_DOUBLE
		case measureInt64:
			return metricspb.MetricDescriptor_GAUGE_INT64
		}

	case view.AggTypeSum:
		// Cumulative types
		switch measureTypeFromMeasure(v.Measure) {
		case measureFloat64:
			return metricspb.MetricDescriptor_CUMULATIVE_DOUBLE
		case measureInt64:
			return metricspb.MetricDescriptor_CUMULATIVE_INT64
		}
	}

	// For all other cases, return unspecified.
	return metricspb.MetricDescriptor_UNSPECIFIED
}

func tagKeysToLabelKeys(tagKeys []tag.Key) []*metricspb.LabelKey {
	labelKeys := make([]*metricspb.LabelKey, 0, len(tagKeys))
	for _, tagKey := range tagKeys {
		labelKeys = append(labelKeys, &metricspb.LabelKey{
			Key: tagKey.Name(),
		})
	}
	return labelKeys
}

func viewDataToTimeseries(vd *view.Data) ([]*metricspb.TimeSeries, error) {
	if vd == nil || len(vd.Rows) == 0 {
		return nil, nil
	}

	// Given that view.Data only contains Start, End
	// the timestamps for all the row data will be the exact same
	// per aggregation. However, the values will differ.
	// Each row has its own tags.
	startTimestamp := timeToProtoTimestamp(vd.Start)
	endTimestamp := timeToProtoTimestamp(vd.End)

	timeseries := make([]*metricspb.TimeSeries, 0, len(vd.Rows))
	// It is imperative that the ordering of "LabelValues" matches those
	// of the Label keys in the metric descriptor.
	for _, row := range vd.Rows {
		labelValues := labelValuesFromTags(row.Tags)
		point := rowToPoint(row, endTimestamp)
		timeseries = append(timeseries, &metricspb.TimeSeries{
			StartTimestamp: startTimestamp,
			LabelValues:    labelValues,
			Points:         []*metricspb.Point{point},
		})
	}

	if len(timeseries) == 0 {
		return nil, nil
	}

	return timeseries, nil
}

func timeToProtoTimestamp(t time.Time) *timestamp.Timestamp {
	unixNano := t.UnixNano()
	return &timestamp.Timestamp{
		Seconds: int64(unixNano / 1e9),
		Nanos:   int32(unixNano % 1e9),
	}
}

func rowToPoint(row *view.Row, endTimestamp *timestamp.Timestamp) *metricspb.Point {
	pt := &metricspb.Point{
		Timestamp: endTimestamp,
	}

	switch data := row.Data.(type) {
	case *view.CountData:
		pt.Value = &metricspb.Point_Int64Value{Int64Value: data.Value}

	case *view.DistributionData:
		pt.Value = &metricspb.Point_DistributionValue{
			DistributionValue: &metricspb.DistributionValue{
				Count:   data.Count,
				Sum:     float64(data.Count) * data.Mean, // because Mean := Sum/Count
				Buckets: exemplarsToDistributionBuckets(data.ExemplarsPerBucket),

				SumOfSquaredDeviation: data.SumOfSquaredDev,
			}}

	case *view.LastValueData:
		pt.Value = &metricspb.Point_DoubleValue{DoubleValue: data.Value}

	case *view.SumData:
		pt.Value = &metricspb.Point_DoubleValue{DoubleValue: data.Value}
	}

	return pt
}

func exemplarsToDistributionBuckets(exemplars []*exemplar.Exemplar) []*metricspb.DistributionValue_Bucket {
	if len(exemplars) == 0 {
		return nil
	}

	distBuckets := make([]*metricspb.DistributionValue_Bucket, 0, len(exemplars))
	for _, exmplr := range exemplars {
		if exmplr == nil {
			continue
		}

		distBuckets = append(distBuckets, &metricspb.DistributionValue_Bucket{
			Count: 1, // TODO: (@odeke-em) examine if OpenCensus-Go stores the count of values in the bucket
			Exemplar: &metricspb.DistributionValue_Exemplar{
				Value:       exmplr.Value,
				Timestamp:   timeToTimestamp(exmplr.Timestamp),
				Attachments: exmplr.Attachments,
			},
		})
	}

	return distBuckets
}

func labelValuesFromTags(tags []tag.Tag) []*metricspb.LabelValue {
	if len(tags) == 0 {
		return nil
	}

	labelValues := make([]*metricspb.LabelValue, 0, len(tags))
	for _, tag_ := range tags {
		labelValues = append(labelValues, &metricspb.LabelValue{
			Value: tag_.Value,
			// It is imperative that we set the "HasValue" attribute,
			// in order to distinguish missing a label from the empty string.
			// https://godoc.org/github.com/census-instrumentation/opencensus-proto/gen-go/metrics/v1#LabelValue.HasValue
			HasValue: true,
		})
	}
	return labelValues
}