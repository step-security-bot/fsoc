package melt

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/apex/log"
	colllogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collspans "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logs "go.opentelemetry.io/proto/otlp/logs/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	spans "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"

	"github.com/cisco-open/fsoc/config"
	"github.com/cisco-open/fsoc/output"
	"github.com/cisco-open/fsoc/platform/api"
)

const (
	pathMetrics                   = "metrics"
	pathLogs                      = "logs"
	pathSpans                     = "trace"
	agentName                     = "sample-datagen"
	agentVersion                  = "0.0.1"
	keyAppdFMMEntityRelationships = "appd.fmm.entity.relations"
	keyAppdIsEvent                = "appd.isevent"
	keyAppdEventType              = "appd.event.type"
)

const (
	DumpFormatHuman = "human"
	DumpFormatText  = "text"
	DumpFormatJson  = "json"
	DumpFormatYaml  = "yaml"
	DumpFormatHex   = "hex"
)

// Exporter -  exporter for entities, metrics and logs
type Exporter struct {
	DumpFunc   func(text string)
	DumpFormat string
	DryRun     bool
}

// ExportMetrics - export metrics
func (exp *Exporter) ExportMetrics(entities []*Entity) error {
	emsr := exp.buildMetricsPayload(entities)

	if emsr.ResourceMetrics == nil {
		log.Info("No metrics to send")
		return nil
	}

	b, _ := json.Marshal(emsr)
	log.Debugf("METRICS: %s", string(b))

	return exp.exportHTTP(pathMetrics, emsr)
}

// ExportLogs - export resource logs
func (exp *Exporter) ExportLogs(entities []*Entity) error {
	elsr := exp.buildLogsPayload(entities)

	if elsr.ResourceLogs == nil {
		log.Info("No logs to send")
		return nil
	}

	b, _ := json.Marshal(elsr)
	log.Debugf("LOGS: %s", string(b))

	return exp.exportHTTP(pathLogs, elsr)
}

// ExportEvents - export events as resource logs
// OTEL does not distibguish between events and logs
func (exp *Exporter) ExportEvents(entities []*Entity) error {
	return exp.ExportLogs(entities)
}

// ExportSpans - export resource spans
func (exp *Exporter) ExportSpans(entities []*Entity) error {
	essr := exp.buildSpansPayload(entities)

	if essr.ResourceSpans == nil {
		log.Info("No spans to send")
		return nil
	}

	b, _ := json.Marshal(essr)
	log.Debugf("SPANS: %s", string(b))

	return exp.exportHTTP(pathSpans, essr)
}

func (exp *Exporter) buildMetricsPayload(entities []*Entity) *collmetrics.ExportMetricsServiceRequest {
	emsr := &collmetrics.ExportMetricsServiceRequest{}

	for _, entity := range entities {
		if len(entity.Metrics) == 0 {
			continue
		}

		rm := &metrics.ResourceMetrics{}
		rm.Resource = &resource.Resource{
			Attributes: toKeyValueList(entity.Attributes),
		}

		exp.addRelationshipsToMetrics(entity.Relationships, rm)

		ilm := &metrics.ScopeMetrics{}

		ml := []*metrics.Metric{}
		for _, m := range entity.Metrics {
			otm := exp.createOtelMetric(m)

			ml = append(ml, otm)
		}
		ilm.Metrics = ml
		ilm.Scope = getInstrumentationScope()

		ilml := []*metrics.ScopeMetrics{ilm}

		rm.ScopeMetrics = ilml

		// append rresource metrics
		emsr.ResourceMetrics = append(emsr.ResourceMetrics, rm)
	}

	return emsr
}

func (exp *Exporter) addRelationships(rels []*Relationship, r *resource.Resource) {
	// add relationships
	if len(rels) > 0 {
		attrib := &common.KeyValue{
			Key: keyAppdFMMEntityRelationships,
		}
		val := &common.AnyValue_ArrayValue{
			ArrayValue: &common.ArrayValue{
				Values: []*common.AnyValue{},
			},
		}
		for _, r := range rels {
			kvlv := &common.AnyValue{
				Value: &common.AnyValue_KvlistValue{
					KvlistValue: &common.KeyValueList{
						Values: toKeyValueList(r.Attributes),
					},
				},
			}
			val.ArrayValue.Values = append(val.ArrayValue.Values, kvlv)
		}
		attrib.Value = &common.AnyValue{
			Value: val,
		}
		r.Attributes = append(r.Attributes, attrib)
	}
}

func (exp *Exporter) addRelationshipsToMetrics(rels []*Relationship, rm *metrics.ResourceMetrics) {
	// add relationships
	if len(rels) > 0 {
		attrib := &common.KeyValue{
			Key: keyAppdFMMEntityRelationships,
		}
		val := &common.AnyValue_ArrayValue{
			ArrayValue: &common.ArrayValue{
				Values: []*common.AnyValue{},
			},
		}
		for _, r := range rels {
			kvlv := &common.AnyValue{
				Value: &common.AnyValue_KvlistValue{
					KvlistValue: &common.KeyValueList{
						Values: toKeyValueList(r.Attributes),
					},
				},
			}
			val.ArrayValue.Values = append(val.ArrayValue.Values, kvlv)
		}
		attrib.Value = &common.AnyValue{
			Value: val,
		}
		rm.Resource.Attributes = append(rm.Resource.Attributes, attrib)
	}
}

func (exp *Exporter) buildLogsPayload(entities []*Entity) *colllogs.ExportLogsServiceRequest {
	elsr := &colllogs.ExportLogsServiceRequest{}

	for _, e := range entities {
		if len(e.Logs) == 0 {
			continue
		}

		rl := &logs.ResourceLogs{}

		rl.Resource = &resource.Resource{
			Attributes: toKeyValueList(e.Attributes),
		}

		exp.addRelationships(e.Relationships, rl.Resource)

		ill := &logs.ScopeLogs{}

		lr := []*logs.LogRecord{}
		for _, l := range e.Logs {
			otl := exp.createOtelLog(l)

			lr = append(lr, otl)
		}
		ill.LogRecords = lr
		ill.Scope = getInstrumentationScope()

		illl := []*logs.ScopeLogs{ill}

		rl.ScopeLogs = illl

		// append resource logs
		elsr.ResourceLogs = append(elsr.ResourceLogs, rl)
	}

	return elsr
}

func (exp *Exporter) buildSpansPayload(entities []*Entity) *collspans.ExportTraceServiceRequest {
	etsr := &collspans.ExportTraceServiceRequest{}

	for _, e := range entities {
		if len(e.Spans) == 0 {
			continue
		}

		rs := &spans.ResourceSpans{}

		rs.Resource = &resource.Resource{
			Attributes: toKeyValueList(e.Attributes),
		}

		sl := []*spans.Span{}
		for _, s := range e.Spans {
			sl = append(sl, exp.createOtelSpan(s))
		}
		ss := &spans.ScopeSpans{
			Spans: sl,
			Scope: getInstrumentationScope(),
		}
		ssl := []*spans.ScopeSpans{ss}
		rs.ScopeSpans = ssl

		// append resource logs
		etsr.ResourceSpans = append(etsr.ResourceSpans, rs)
	}

	return etsr
}

func (exp *Exporter) createOtelMetric(m *Metric) *metrics.Metric {
	otm := &metrics.Metric{
		Name: m.TypeName,
	}

	switch m.ContentType {
	case "sum":
		mAttribs := toKeyValueList(m.Attributes)

		s := &metrics.Sum{
			AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED,
			IsMonotonic:            m.IsMonotonic,
		}
		switch m.AggregationTemporality {
		case AggregationTemporalityDelta:
			s.AggregationTemporality = metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA
		case AggregationTemporalityCumulative:
			s.AggregationTemporality = metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
		}
		for _, dp := range m.DataPoints {
			sdp := &metrics.NumberDataPoint{
				StartTimeUnixNano: uint64(dp.StartTime),
				TimeUnixNano:      uint64(dp.EndTime),
				Attributes:        mAttribs,
			}
			switch m.Type {
			case "long":
				sdp.Value = &metrics.NumberDataPoint_AsInt{AsInt: int64(dp.Value)}
			case "double":
				sdp.Value = &metrics.NumberDataPoint_AsDouble{AsDouble: dp.Value}
			}
			s.DataPoints = append(s.DataPoints, sdp)
		}
		otm.Data = &metrics.Metric_Sum{Sum: s}

		return otm

	case "gauge":
		mAttribs := toKeyValueList(m.Attributes)
		s := &metrics.Gauge{}

		for _, dp := range m.DataPoints {
			otdp := &metrics.NumberDataPoint{
				StartTimeUnixNano: uint64(dp.StartTime),
				TimeUnixNano:      uint64(dp.EndTime),
				Attributes:        mAttribs,
			}

			switch m.Type {
			case "long":
				otdp.Value = &metrics.NumberDataPoint_AsInt{AsInt: int64(dp.Value)}
			case "double":
				otdp.Value = &metrics.NumberDataPoint_AsDouble{AsDouble: dp.Value}
			}

			s.DataPoints = append(s.DataPoints, otdp)
		}

		otm.Data = &metrics.Metric_Gauge{Gauge: s}

		return otm
	}

	log.Errorf("unsuported metrics type: %s", m.ContentType)

	return nil
}

func (exp *Exporter) createOtelLog(l *Log) *logs.LogRecord {
	// indicators for events
	if l.IsEvent {
		l.Attributes[keyAppdIsEvent] = "true"
		l.Attributes[keyAppdEventType] = l.TypeName
	}

	lAttribs := toKeyValueList(l.Attributes)

	otl := &logs.LogRecord{
		Body: &common.AnyValue{
			Value: &common.AnyValue_StringValue{
				StringValue: l.Body,
			},
		},
		TimeUnixNano: uint64(l.Timestamp),
		Attributes:   lAttribs,
	}
	if l.Severity != "" {
		otl.SeverityText = l.Severity
	}

	return otl
}

func (exp *Exporter) createOtelSpan(t *Span) *spans.Span {
	ots := &spans.Span{
		Name:              t.Name,
		TraceId:           []byte(t.TraceID),
		SpanId:            []byte(t.SpanID),
		TraceState:        t.TraceState,
		ParentSpanId:      []byte(t.ParentSpanID),
		Kind:              spans.Span_SpanKind(t.Kind),
		StartTimeUnixNano: uint64(t.StartTime),
		EndTimeUnixNano:   uint64(t.EndTime),
		Attributes:        toKeyValueList(t.Attributes),
	}

	// events
	for _, e := range t.Events {
		ots.Events = append(ots.Events, &spans.Span_Event{
			TimeUnixNano: uint64(e.Timestamp),
			Name:         e.Name,
			Attributes:   toKeyValueList(e.Attributes),
		})
	}

	// links
	for _, l := range t.Links {
		ots.Links = append(ots.Links, &spans.Span_Link{
			TraceId:    []byte(l.TraceID),
			SpanId:     []byte(l.SpanID),
			TraceState: l.TraceState,
			Attributes: toKeyValueList(l.Attributes),
		})
	}

	// status
	if t.Status != nil {
		ots.Status = &spans.Status{
			Message: t.Status.Message,
			Code:    spans.Status_StatusCode(t.Status.Code),
		}
	}
	return ots
}

func (exp *Exporter) exportHTTP(path string, m protoreflect.ProtoMessage) error {

	options := api.Options{
		Headers: map[string]string{
			"Content-Type": "application/x-protobuf",
			"Accept":       "application/x-protobuf",
		},
	}

	// marshal into protobuf
	data, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal MELT data: %w", err)
	}

	// dump data if requested
	if exp.DumpFunc != nil {
		dumpPayload(m, exp.DumpFormat, exp.DumpFunc)
	}

	// send data
	if !exp.DryRun {
		apiPath := "data/v1/" + path
		// post to API
		err = api.HTTPPost(apiPath, data, nil, &options)
		if err != nil {
			hintAboutPermissions(err)
			return err
		}

		// log traceresponse
		tr := ""
		if trh, ok := options.ResponseHeaders["Traceresponse"]; ok {
			tr = trh[0] // first value only
		}
		log.WithFields(log.Fields{
			"kind":           path,
			"path":           apiPath,
			"trace_response": tr,
		}).Info("Sent MELT data")
	}

	return nil
}

func toKeyValueList(a map[string]interface{}) []*common.KeyValue {
	attribs := []*common.KeyValue{}
	for k, v := range a {
		otVal := &common.AnyValue{}
		vt := reflect.ValueOf(v)
		switch vt.Kind() {
		case reflect.Bool:
			otVal.Value = &common.AnyValue_BoolValue{
				BoolValue: vt.Bool(),
			}
		case reflect.Int, reflect.Int8, reflect.Int32, reflect.Int64:
			otVal.Value = &common.AnyValue_IntValue{
				IntValue: vt.Int(),
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint32, reflect.Uint64:
			otVal.Value = &common.AnyValue_IntValue{
				IntValue: int64(vt.Uint()),
			}
		case reflect.Float32, reflect.Float64:
			otVal.Value = &common.AnyValue_DoubleValue{
				DoubleValue: float64(vt.Float()),
			}
		case reflect.String:
			otVal.Value = &common.AnyValue_StringValue{
				StringValue: vt.String(),
			}
		default:
			if v == nil {
				log.Warnf("Value not set for attribute: %s", k)
				continue
			}
			otVal.Value = &common.AnyValue_StringValue{
				StringValue: fmt.Sprintf("%v", vt.Interface()),
			}
		}
		value := &common.KeyValue{
			Key:   k,
			Value: otVal,
		}

		attribs = append(attribs, value)

	}
	return attribs
}

func getInstrumentationScope() *common.InstrumentationScope {
	return &common.InstrumentationScope{
		Name:    agentName,
		Version: agentVersion,
	}
}

func dumpPayload(m proto.Message, format string, writer func(string)) {
	var s string
	var b []byte
	var err error

	switch format {
	case DumpFormatHuman:
		s = prototext.Format(m)
	case DumpFormatText:
		b, err = prototext.Marshal(m)
	case DumpFormatJson:
		b, err = json.MarshalIndent(m, "", output.JsonIndent)
	case DumpFormatYaml:
		b, err = yaml.Marshal(m)
	case DumpFormatHex:
		b, err = proto.Marshal(m)
		if err == nil {
			s = hex.Dump(b)
			b = nil
		}
	}

	// abort if marshalling failed
	if err != nil {
		log.Fatalf("Unable to marshal MELT data to %s format: %v", format, err)
	}

	// convert bytes to string
	if s == "" && b != nil {
		s = string(b) + "\n"
	}

	writer(s)
}

func hintAboutPermissions(err error) {
	// provide a detailed hint if it is a permissions error and using a profile that is not an agent principal
	var statusError *api.HttpStatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusForbidden {
		// provide more info if not using an agent principal auth type in the profile
		ctx := config.GetCurrentContext()
		if ctx.AuthMethod != config.AuthMethodAgentPrincipal {
			log.Warnf(`Hint: this command requires a profile with ingestion permissions. `+
				`Usually, this would be a profile that uses the "agent-principal" auth method; `+
				`the selected profile uses %q instead. In general, any principal with `+
				`ingestion permissions can be used, regardless of the auth type; `+
				`these permissions can be assigned by adding the "iam:agent" role to a principal using the "fsoc iam-role-binding" command.`,
				ctx.AuthMethod)
		}
	}
}
