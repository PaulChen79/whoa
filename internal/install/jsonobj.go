package install

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// object is a JSON object that remembers the order its keys arrived in.
//
// whoa edits a settings file it does not own and is not the only writer of.
// Go's map-based decoding would hand the file back with its keys sorted,
// turning an install into a whole-file diff, so key order is carried through
// by hand.
type object struct {
	keys   []string
	values map[string]json.RawMessage
}

func newObject() *object {
	return &object{values: map[string]json.RawMessage{}}
}

func (o *object) UnmarshalJSON(b []byte) error {
	o.keys = nil
	o.values = map[string]json.RawMessage{}

	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected an object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if _, seen := o.values[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.values[key] = raw
	}
	_, err = dec.Token() // closing brace
	return err
}

func (o *object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(o.values[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *object) get(key string) (json.RawMessage, bool) {
	v, ok := o.values[key]
	return v, ok
}

func (o *object) set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, seen := o.values[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.values[key] = raw
	return nil
}

func (o *object) delete(key string) {
	if _, seen := o.values[key]; !seen {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *object) empty() bool { return len(o.keys) == 0 }
