package golden_test

type tc struct {
	pkg    string
	name   string
	method string // POST /check when empty
	target string
	header map[string]string
	body   string
	status int
	// errs lists "location code" pairs: a JSON Pointer for body errors,
	// "?name" for parameters.
	errs []string
	// kinDiffers explains why kin-openapi gives the other verdict.
	kinDiffers string
}

func body(pkg, name, b string, status int, errs ...string) tc {
	return tc{pkg: pkg, name: name, body: b, status: status, errs: errs}
}

func (c tc) differs(reason string) tc { c.kinDiffers = reason; return c }

var cases = []tc{
	// required
	body("required", "all present", `{"name":"a","count":0,"enabled":false,"tags":[],"nick":null}`, 204),
	body(
		"required",
		"empty strings and zeros are present",
		`{"name":"","count":0,"enabled":false,"tags":[],"nick":"x"}`,
		204,
	),
	body(
		"required",
		"all absent",
		`{}`,
		422,
		"/name required",
		"/count required",
		"/enabled required",
		"/tags required",
		"/nick required",
	),
	body(
		"required",
		"null on non-nullable",
		`{"name":null,"count":0,"enabled":false,"tags":[],"nick":null}`,
		422,
		"/name type",
	),
	body(
		"required",
		"null array element",
		`{"name":"a","count":0,"enabled":false,"tags":["a",null],"nick":null}`,
		422,
		"/tags/1 type",
	),
	body(
		"required",
		"null on optional",
		`{"name":"a","count":0,"enabled":false,"tags":[],"nick":null,"note":null}`,
		422,
		"/note type",
	),
	body(
		"required",
		"constraint after presence",
		`{"name":"a","count":-1,"enabled":false,"tags":[],"nick":null,"note":"abcd"}`,
		422,
		"/count minimum",
		"/note maxLength",
	),
	body(
		"required",
		"wrong JSON type",
		`{"name":"a","count":"1","enabled":false,"tags":[],"nick":null}`,
		400,
		"/count type",
	),
	body("required", "readOnly is not required", `{"name":"a","count":0,"enabled":false,"tags":[],"nick":null}`, 204),

	// numbers (3.0 boolean exclusive bounds)
	body("numbers", "in range", `{"age":18,"score":0.5,"step":10,"ratio":0.3,"small":10}`, 204),
	body("numbers", "below minimum", `{"age":17}`, 422, "/age minimum"),
	body("numbers", "above maximum", `{"age":131}`, 422, "/age maximum"),
	body("numbers", "exclusive bounds", `{"score":0}`, 422, "/score exclusiveMinimum"),
	body("numbers", "exclusive maximum", `{"score":1}`, 422, "/score exclusiveMaximum"),
	body("numbers", "integer multipleOf", `{"step":7}`, 422, "/step multipleOf"),
	body("numbers", "decimal multipleOf", `{"ratio":0.35}`, 422, "/ratio multipleOf"),
	body("numbers", "fractional maximum on an integer", `{"small":11}`, 422, "/small maximum"),
	body("numbers", "fraction for an integer", `{"age":18.5}`, 400, "/age type"),
	// JSON Schema and kin-openapi define integer by value, not spelling.
	body("numbers", "integral number for an integer", `{"age":18.0,"step":1e1,"small":10.0}`, 204),
	body("numbers", "integral number keeps its constraints", `{"step":7.0}`, 422, "/step multipleOf"),
	body("numbers", "integral number out of range", `{"small":3e9}`, 400, "/small format"),
	body("numbers", "int32 overflow", `{"small":3000000000}`, 400, "/small format"),
	// No required member and no minProperties: only the root check rejects a
	// null body, which decodes into the Go struct without an error.
	body("numbers", "null body on a non-nullable object", `null`, 422, "/ type"),
	body("numbers", "null body with spaces", " null ", 422, "/ type"),

	// numbers31 (3.1 numeric exclusive bounds, integer|null)
	body("numbers31", "in range", `{"price":0.01,"qty":null,"level":5}`, 204),
	body("numbers31", "exclusive minimum", `{"price":0}`, 422, "/price exclusiveMinimum"),
	body("numbers31", "exclusive maximum", `{"price":1000}`, 422, "/price exclusiveMaximum"),
	body("numbers31", "nullable integer below minimum", `{"qty":0}`, 422, "/qty minimum"),
	body("numbers31", "above maximum", `{"level":6}`, 422, "/level maximum"),

	// strings
	body("strings", "valid", `{"name":"ééé","slug":"a-b","notBlank":" x ","color":"#a0B1c2","word":"Abc"}`, 204),
	body("strings", "too short", `{"name":"a"}`, 422, "/name minLength"),
	body("strings", "length counts runes", `{"name":"éééééé"}`, 422, "/name maxLength"),
	body(
		"strings",
		"pattern",
		`{"slug":"Bad Slug","color":"#12345g","word":"abc"}`,
		422,
		"/slug pattern",
		"/color pattern",
		"/word pattern",
	),
	body("strings", "blank", `{"notBlank":"   "}`, 422, "/notBlank pattern"),
	body("strings", "ECMA \\s includes no-break space", `{"notBlank":" "}`, 422, "/notBlank pattern").
		differs("kin compiles patterns with Go regexp, where \\S matches U+00A0; JSON Schema patterns are ECMA-262, where U+00A0 is whitespace"),

	// formats
	body(
		"formats",
		"valid",
		`{"id":"7f9c24e8-3b12-4fef-91e0-3a5b0c6d1e2f","email":"a@b.co","site":"https://x.dev/a","at":"2026-11-07T10:00:00Z","day":"2026-11-07","v4":"10.0.0.1","v6":"::1","host":"api.x.dev","typedId":"7f9c24e8-3b12-4fef-91e0-3a5b0c6d1e2f","typedAt":"2026-11-07T10:00:00Z","typedDay":"2026-11-07","typedEmail":"a@b.co"}`,
		204,
	),
	body(
		"formats",
		"string formats are 422 and accumulate",
		`{"id":"x","email":"a@b","site":"example.com","at":"2026-11-07","v4":"1.2.3","v6":"1.2.3.4","host":"-bad"}`,
		422,
		"/id format",
		"/email format",
		"/site format",
		"/at format",
		"/v4 format",
		"/v6 format",
		"/host format",
	),
	body("formats", "uuid", `{"id":"7f9c24e83b124fef91e03a5b0c6d1e2f"}`, 422, "/id format"),
	body("formats", "email", `{"email":"A <a@b.co>"}`, 422, "/email format"),
	body("formats", "uri", `{"site":"/relative"}`, 422, "/site format"),
	body("formats", "date-time", `{"at":"2026-11-07T10:00:00"}`, 422, "/at format"),
	body("formats", "ipv4", `{"v4":"256.0.0.1"}`, 422, "/v4 format"),
	body("formats", "ipv6", `{"v6":"2001:db8:::1"}`, 422, "/v6 format"),
	body("formats", "hostname", `{"host":"a_b.dev"}`, 422, "/host format"),
	body("formats", "calendar date", `{"day":"2026-02-30"}`, 422, "/day format").
		differs("kin checks date with a regexp that accepts February 30"),
	body("formats", "typed uuid fails decoding", `{"typedId":"x"}`, 400, "/typedId format"),
	body("formats", "typed date-time fails decoding", `{"typedAt":"yesterday"}`, 400, "/typedAt format"),
	body("formats", "typed date fails decoding", `{"typedDay":"2026-13-01"}`, 400, "/typedDay format"),

	// enums
	body(
		"enums",
		"valid",
		`{"status":"active","statuses":["paused"],"mode":"fast","priority":2,"kind":"fixed","flag":true}`,
		204,
	),
	// The named integer type decodes 2.0 like Go's integers: by value.
	body("enums", "integer enum as an integral number", `{"priority":2.0}`, 204),
	body("enums", "integer enum as a fraction", `{"priority":2.5}`, 400, "/priority type"),
	body(
		"enums",
		"every enum",
		`{"status":"deleted","statuses":["active","x"],"mode":"medium","priority":4,"kind":"other","flag":false}`,
		422,
		"/status enum",
		"/statuses/1 enum",
		"/mode enum",
		"/priority enum",
		"/kind enum",
		"/flag enum",
	),

	// arrays
	body("arrays", "valid", `{"tags":["a","b"],"items":[{"sku":"a"},{"sku":"b"}],"matrix":[[0,1],[2]]}`, 204),
	body("arrays", "minItems", `{"tags":[]}`, 422, "/tags minItems"),
	body("arrays", "maxItems", `{"tags":["a","b","c","d"]}`, 422, "/tags maxItems"),
	body("arrays", "uniqueItems on strings", `{"tags":["a","a"]}`, 422, "/tags uniqueItems"),
	body("arrays", "uniqueItems on objects", `{"items":[{"sku":"a"},{"sku":"a"}]}`, 422, "/items uniqueItems"),
	body("arrays", "item constraints", `{"tags":["abcde"],"items":[{}],"matrix":[[1,2,3],[-1]]}`, 422,
		"/tags/0 maxLength", "/items/0/sku required", "/matrix/0 maxItems", "/matrix/1/0 minimum"),

	// nested
	body(
		"nested",
		"valid",
		`{"name":"n","home":{"city":"c"},"others":[{"city":"d","zip":"12345"}],"inline":{"code":1},"labels":{"a":"b"},"byName":{"x":{"city":"y"}}}`,
		204,
	),
	body("nested", "allOf members are required", `{}`, 422, "/name required", "/home required"),
	body(
		"nested",
		"nested violations",
		`{"name":"01234567890","home":{},"others":[{"city":""}],"inline":{},"labels":{"a":""},"byName":{"x/y":{"zip":"1"}}}`,
		422,
		"/name maxLength",
		"/home/city required",
		"/others/0/city minLength",
		"/inline/code required",
		"/labels/a minLength",
		"/byName/x~1y/city required",
		"/byName/x~1y/zip pattern",
	),
	body(
		"nested",
		"inline maximum",
		`{"name":"n","home":{"city":"c"},"inline":{"code":10}}`,
		422,
		"/inline/code maximum",
	),

	// nullable (3.0)
	body(
		"nullable",
		"nulls where allowed",
		`{"maybe":null,"must":"ab","opt":null,"inner":null,"list":[null,"a"]}`,
		204,
	),
	body("nullable", "required nullable is still required", `{"must":"ab"}`, 422, "/maybe required"),
	body(
		"nullable",
		"constraints apply to non-null",
		`{"maybe":"a","must":"ab","opt":"abc","inner":{},"list":[""]}`,
		422,
		"/maybe minLength",
		"/opt maxLength",
		"/inner/v required",
		"/list/0 minLength",
	),
	body("nullable", "null on non-nullable", `{"maybe":null,"must":null}`, 422, "/must type"),
	body("nullable", "null body", `null`, 422, "/ type"),

	// nullable31 (3.1 type arrays)
	body("nullable31", "nulls where allowed", `{"maybe":null,"must":"ab","count":null,"inner":null,"tags":null}`, 204),
	body(
		"nullable31",
		"constraints apply to non-null",
		`{"maybe":"a","must":"ab","count":4,"inner":{"v":0},"tags":[null]}`,
		422,
		"/maybe minLength",
		"/count maximum",
		"/inner/v minimum",
		"/tags/0 type",
	),
	body("nullable31", "required nullable is still required", `{"must":"ab"}`, 422, "/maybe required"),

	// nullabletype (oapi-codegen nullable-type: nullable.Nullable)
	body("nullabletype", "null and absent", `{"maybe":null}`, 204),
	body("nullabletype", "values", `{"maybe":"ab","opt":3,"flag":true}`, 204),
	body("nullabletype", "required nullable is still required", `{}`, 422, "/maybe required"),
	body(
		"nullabletype",
		"constraints apply to values",
		`{"maybe":"a","opt":4}`,
		422,
		"/maybe minLength",
		"/opt maximum",
	),

	// unions
	body(
		"unions",
		"discriminated variants",
		`{"auth":{"type":"basic","user":"u"},"pet":{"meows":true},"source":{"path":"a","header":"b"}}`,
		204,
	),
	body("unions", "unknown discriminator value", `{"auth":{"type":"oauth"}}`, 422, "/auth/type discriminator"),
	body("unions", "missing discriminator", `{"auth":{"user":"u"}}`, 422, "/auth/type required"),
	body(
		"unions",
		"selected variant is validated",
		`{"auth":{"type":"bearer","token":"short"}}`,
		422,
		"/auth/token minLength",
	),
	body(
		"unions",
		"variant decode error keeps its pointer",
		`{"auth":{"type":"basic","user":5}}`,
		422,
		"/auth/user type",
	),
	body("unions", "oneOf matches none", `{"auth":{"type":"basic","user":"u"},"pet":{}}`, 422, "/pet oneOf"),
	body(
		"unions",
		"oneOf matches both",
		`{"auth":{"type":"basic","user":"u"},"pet":{"meows":true,"barks":true}}`,
		422,
		"/pet oneOf",
	),
	body("unions", "anyOf matches none", `{"auth":{"type":"basic","user":"u"},"source":{}}`, 422, "/source anyOf"),
	body("unions", "scalar oneOf integer", `{"auth":{"type":"basic","user":"u"},"prim":5}`, 204),
	body("unions", "scalar oneOf integral number", `{"auth":{"type":"basic","user":"u"},"prim":5.0}`, 204),
	body("unions", "scalar oneOf fraction", `{"auth":{"type":"basic","user":"u"},"prim":5.5}`, 422, "/prim oneOf"),

	// additional
	body("additional", "valid", `{"name":"x","meta":{"a":"1","d~e":"22"}}`, 204),
	body("additional", "minProperties", `{}`, 422, "/ minProperties"),
	body(
		"additional",
		"unknown members with escaped pointers",
		`{"name":"x","a/b~c":1,"meta":{"zz":"1","d~e":"1"}}`,
		422,
		"/meta/d~0e minLength",
		"/meta/zz additionalProperties",
		"/a~1b~0c additionalProperties",
	),
	body("additional", "maxProperties", `{"meta":{"a":"1","b/c":"2","d~e":"33"}}`, 422, "/meta maxProperties"),

	// params
	{
		pkg:    "params",
		name:   "valid",
		method: "GET",
		target: "/envs/prod/items?limit=50&sort=asc",
		header: map[string]string{"X-Request-Tag": "t1"},
		status: 204,
	},
	{
		pkg:    "params",
		name:   "query maximum",
		method: "GET",
		target: "/envs/prod/items?limit=101",
		status: 400,
		errs:   []string{"?limit maximum"},
	},
	{
		pkg:    "params",
		name:   "query minimum and enum",
		method: "GET",
		target: "/envs/prod/items?limit=0&sort=up",
		status: 400,
		errs:   []string{"?limit minimum", "?sort enum"},
	},
	{
		pkg:    "params",
		name:   "query that does not bind",
		method: "GET",
		target: "/envs/prod/items?limit=abc",
		status: 400,
		errs:   []string{"?limit type"},
	},
	{
		pkg:    "params",
		name:   "path pattern",
		method: "GET",
		target: "/envs/-x/items",
		status: 400,
		errs:   []string{"?environment pattern"},
	},
	{
		pkg:    "params",
		name:   "header maxLength",
		method: "GET",
		target: "/envs/prod/items",
		header: map[string]string{"X-Request-Tag": "much-too-long"},
		status: 400,
		errs:   []string{"?X-Request-Tag maxLength"},
	},
}
