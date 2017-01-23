package conf

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/segmentio/objconv"
	"github.com/segmentio/objconv/json"
)

type NodeKind int

const (
	ScalarNode NodeKind = iota
	ArrayNode
	MapNode
)

type Node interface {
	Kind() NodeKind

	Value() interface{}

	String() string

	EncodeValue(objconv.Encoder) error

	DecodeValue(objconv.Decoder) error
}

func EqualNode(n1 Node, n2 Node) bool {
	if n1 == nil || n2 == nil {
		return n1 == n2
	}

	k1 := n1.Kind()
	k2 := n2.Kind()

	if k1 != k2 {
		return false
	}

	switch k1 {
	case ArrayNode:
		return equalNodeArray(n1.(Array), n2.(Array))
	case MapNode:
		return equalNodeMap(n1.(Map), n2.(Map))
	default:
		return equalNodeScalar(n1.(Scalar), n2.(Scalar))
	}
}

func equalNodeArray(a1 Array, a2 Array) bool {
	n1 := a1.Len()
	n2 := a2.Len()

	if n1 != n2 {
		return false
	}

	for i := 0; i != n1; i++ {
		if !EqualNode(a1.Item(i), a2.Item(i)) {
			return false
		}
	}

	return true
}

func equalNodeMap(m1 Map, m2 Map) bool {
	n1 := m1.Len()
	n2 := m2.Len()

	if n1 != n2 {
		return false
	}

	for _, item := range m1.Items() {
		if !EqualNode(item.Value, m2.Item(item.Name)) {
			return false
		}
	}

	return true
}

func equalNodeScalar(s1 Scalar, s2 Scalar) bool {
	v1 := s1.value.IsValid()
	v2 := s2.value.IsValid()

	if !v1 || !v2 {
		return v1 == v2
	}

	t1 := s1.value.Type()
	t2 := s2.value.Type()

	if t1 != t2 {
		return false
	}

	switch t1 {
	case timeTimeType:
		return s1.Value().(time.Time).Equal(s2.Value().(time.Time))
	}

	return reflect.DeepEqual(s1.Value(), s2.Value())
}

func MakeNode(cfg interface{}) Node {
	return makeNode(reflect.ValueOf(cfg))
}

func makeNode(v reflect.Value) Node {
	if !v.IsValid() {
		return makeNodeScalar(v)
	}

	t := v.Type()

	switch t {
	case timeTimeType, timeDurationType:
		return makeNodeScalar(v)
	}

	if _, ok := objconv.AdapterOf(t); ok {
		return makeNodeScalar(v)
	}

	switch {
	case
		t.Implements(objconvValueDecoderInterface),
		t.Implements(textUnmarshalerInterface):
		return makeNodeScalar(v)
	}

	switch t.Kind() {
	case reflect.Struct:
		return makeNodeStruct(v, t)

	case reflect.Map:
		return makeNodeMap(v, t)

	case reflect.Slice:
		return makeNodeSlice(v, t)

	case reflect.Ptr:
		return makeNodePtr(v, t)

	default:
		return makeNodeScalar(v)
	}
}

func makeNodeStruct(v reflect.Value, t reflect.Type) (m Map) {
	m.value = v
	m.items = newMapItems()

	for i, n := 0, v.NumField(); i != n; i++ {
		fv := v.Field(i)
		ft := t.Field(i)

		if !isExported(ft) {
			continue
		}

		name, help := ft.Tag.Get("conf"), ft.Tag.Get("help")
		switch name {
		case "-":
			continue
		case "":
			name = ft.Name
		}

		m.items.push(MapItem{
			Name:  name,
			Help:  help,
			Value: makeNode(fv),
		})
	}

	return
}

func makeNodeMap(v reflect.Value, t reflect.Type) (m Map) {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}

	m.value = v
	m.items = newMapItems()

	for _, key := range v.MapKeys() {
		m.items.push(MapItem{
			Name:  key.String(), // only string keys are supported for now
			Value: makeNode(v.MapIndex(key)),
		})
	}

	sort.Sort(m.items)
	return
}

func makeNodeSlice(v reflect.Value, t reflect.Type) (a Array) {
	n := v.Len()
	a.value = v
	a.items = newArrayItems()

	for i := 0; i != n; i++ {
		a.items.push(makeNode(v.Index(i)))
	}

	return
}

func makeNodePtr(v reflect.Value, t reflect.Type) Node {
	if v.IsNil() {
		p := reflect.New(t.Elem())

		if v.CanSet() {
			v.Set(p)
		}

		v = p
	}
	return makeNode(v.Elem())
}

func makeNodeScalar(value reflect.Value) (s Scalar) {
	s.value = value
	return
}

type Scalar struct {
	value reflect.Value
}

func (s Scalar) Kind() NodeKind {
	return ScalarNode
}

func (s Scalar) Value() interface{} {
	if !s.value.IsValid() {
		return nil
	}
	return s.value.Interface()
}

func (s Scalar) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s Scalar) EncodeValue(e objconv.Encoder) error {
	return e.Encode(s.Value())
}

func (s Scalar) DecodeValue(d objconv.Decoder) error {
	return d.Decode(s.value.Addr().Interface())
}

type Array struct {
	value reflect.Value
	items *arrayItems
}

func (a Array) Kind() NodeKind {
	return ArrayNode
}

func (a Array) Value() interface{} {
	if !a.value.IsValid() {
		return nil
	}
	return a.value.Interface()
}

func (a Array) Items() []Node {
	if a.items == nil {
		return nil
	}
	return a.items.items()
}

func (a Array) Item(i int) Node {
	return a.items.index(i)
}

func (a Array) Len() int {
	if a.items == nil {
		return 0
	}
	return a.items.len()
}

func (a Array) String() string {
	if a.Len() == 0 {
		return "[ ]"
	}
	b := &bytes.Buffer{}
	b.WriteByte('[')

	for i, item := range a.Items() {
		if i != 0 {
			b.WriteString(", ")
		}
		b.WriteString(item.String())
	}

	b.WriteByte(']')
	return b.String()
}

func (a Array) EncodeValue(e objconv.Encoder) (err error) {
	i := 0
	return e.EncodeArray(a.Len(), func(e objconv.Encoder) (err error) {
		if err = a.Item(i).EncodeValue(e); err != nil {
			return
		}
		i++
		return
	})
}

func (a Array) DecodeValue(d objconv.Decoder) (err error) {
	a.pop(a.Len())
	return d.DecodeArray(func(d objconv.Decoder) (err error) {
		if err = a.push().DecodeValue(d); err != nil {
			a.pop(1)
		}
		return
	})
}

func (a Array) push() Node {
	i := a.Len()
	a.value.Set(reflect.Append(a.value, reflect.Zero(a.value.Type().Elem())))
	a.items.push(makeNode(a.value.Index(i)))
	return a.items.index(i)
}

func (a Array) pop(n int) {
	if n != 0 {
		i := a.Len() - n
		a.value.Set(a.value.Slice(0, i))
		a.items.pop(n)
	}
}

type Map struct {
	value reflect.Value
	items *mapItems
}

func (m Map) Kind() NodeKind {
	return MapNode
}

func (m Map) Value() interface{} {
	if !m.value.IsValid() {
		return nil
	}
	return m.value.Interface()
}

func (m Map) Items() []MapItem {
	if m.items == nil {
		return nil
	}
	return m.items.items()
}

func (m Map) Item(name string) Node {
	if m.items == nil {
		return nil
	}
	return m.items.get(name)
}

func (m Map) Len() int {
	if m.items == nil {
		return 0
	}
	return m.items.len()
}

func (m Map) String() string {
	if m.Len() == 0 {
		return "{ }"
	}

	b := &bytes.Buffer{}
	b.WriteString("{ ")

	for i, item := range m.Items() {
		if i != 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%s: %s", item.Name, item.Value)

		if len(item.Help) != 0 {
			fmt.Fprintf(b, " (%s)", item.Help)
		}
	}

	b.WriteString(" }")
	return b.String()
}

func (m Map) EncodeValue(e objconv.Encoder) error {
	i := 0
	return e.EncodeMap(m.Len(), func(ke objconv.Encoder, ve objconv.Encoder) (err error) {
		item := m.items.nodes[i]

		if err = ke.EncodeString(item.Name); err != nil {
			return
		}
		if err = item.Value.EncodeValue(ve); err != nil {
			return
		}

		i++
		return
	})
}

func (m Map) DecodeValue(d objconv.Decoder) error {
	return d.DecodeMap(func(kd objconv.Decoder, vd objconv.Decoder) (err error) {
		var key string

		if err = kd.Decode(&key); err != nil {
			return
		}

		if item := m.Item(key); item != nil {
			return item.DecodeValue(vd)
		}

		if m.value.Kind() == reflect.Struct {
			return vd.Decode(nil) // discard
		}

		name := reflect.ValueOf(key)
		node := makeNode(reflect.New(m.value.Type().Elem()).Elem())

		if err = node.DecodeValue(vd); err != nil {
			return
		}

		m.value.SetMapIndex(name, reflect.ValueOf(node.Value()))
		m.items.put(MapItem{
			Name:  key,
			Value: makeNode(m.value.MapIndex(name)),
		})
		return
	})
}

type MapItem struct {
	Name  string
	Help  string
	Value Node
}

type arrayItems struct {
	nodes []Node
}

func newArrayItems(nodes ...Node) *arrayItems {
	return &arrayItems{nodes}
}

func (a *arrayItems) push(n Node) {
	a.nodes = append(a.nodes, n)
}

func (a *arrayItems) pop(n int) {
	a.nodes = a.nodes[:n]
}

func (a *arrayItems) len() int {
	return len(a.nodes)
}

func (a *arrayItems) index(i int) Node {
	return a.nodes[i]
}

func (a *arrayItems) items() []Node {
	return a.nodes
}

type mapItems struct {
	nodes []MapItem
}

func newMapItems(nodes ...MapItem) *mapItems {
	return &mapItems{nodes}
}

func (m *mapItems) get(name string) Node {
	if i := m.index(name); i >= 0 {
		return m.nodes[i].Value
	}
	return nil
}

func (m *mapItems) index(name string) int {
	for i, node := range m.nodes {
		if node.Name == name {
			return i
		}
	}
	return -1
}

func (m *mapItems) len() int {
	return len(m.nodes)
}

func (m *mapItems) items() []MapItem {
	return m.nodes
}

func (m *mapItems) push(item MapItem) {
	m.nodes = append(m.nodes, item)
}

func (m *mapItems) put(item MapItem) {
	if i := m.index(item.Name); i >= 0 {
		m.nodes[i] = item
	} else {
		m.push(item)
	}
}

func (m *mapItems) Less(i int, j int) bool {
	return m.nodes[i].Name < m.nodes[j].Name
}

func (m *mapItems) Swap(i int, j int) {
	m.nodes[i], m.nodes[j] = m.nodes[j], m.nodes[i]
}

func (m *mapItems) Len() int {
	return len(m.nodes)
}
