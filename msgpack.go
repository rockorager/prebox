package prebox

import "fmt"

func expectString(v interface{}) (string, error) {
	str, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("invalid argument. Expected string. Got %T", v)
	}
	return str, nil
}

func expectInt(v interface{}) (int, error) {
	switch v := v.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid argument. Expected int. Got %T", v)
	}
}

func expectLen(l int, v []interface{}) error {
	if len(v) != l {
		return fmt.Errorf("invalid argument. Expected len=%d. Got len=%d", l, len(v))
	}
	return nil
}

func validateMsg(msg []interface{}) (int, int, string, []interface{}, error) {
	switch len(msg) {
	case 3:
		kind, method, args, err := validateNotification(msg)
		if err != nil {
			return 0, 0, "", []interface{}{}, err
		}
		return kind, 0, method, args, nil
	default:
		return validateReqOrResp(msg)
	}
}

func validateReqOrResp(msg []interface{}) (int, int, string, []interface{}, error) {
	err := expectLen(4, msg)
	if err != nil {
		return 0, 0, "", []interface{}{}, err
	}
	kind, err := expectInt(msg[0])
	if err != nil {
		return 0, 0, "", []interface{}{}, err
	}
	id, err := expectInt(msg[1])
	if err != nil {
		return 0, 0, "", []interface{}{}, err
	}
	method, err := expectString(msg[2])
	if err != nil {
		return 0, 0, "", []interface{}{}, err
	}
	args, ok := msg[3].([]interface{})
	if !ok {
		return 0, 0, "", []interface{}{}, fmt.Errorf("invalid format. Expected []interface{}. Got %T", msg[3])
	}
	return kind, id, method, args, nil
}

func validateNotification(msg []interface{}) (int, string, []interface{}, error) {
	err := expectLen(3, msg)
	if err != nil {
		return 0, "", []interface{}{}, err
	}
	kind, err := expectInt(msg[0])
	if err != nil {
		return 0, "", []interface{}{}, err
	}
	method, err := expectString(msg[1])
	if err != nil {
		return 0, "", []interface{}{}, err
	}
	args, ok := msg[2].([]interface{})
	if !ok {
		return 0, "", []interface{}{}, fmt.Errorf("invalid format. Expected []interface{}. Got %T", msg[3])
	}
	return kind, method, args, nil
}
