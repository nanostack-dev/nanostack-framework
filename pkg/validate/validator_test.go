package validate

import (
	"testing"
)

type TestUser struct {
	Password   string `validate:"strongpassword"`
	Name       string `validate:"notblank"`
	Permission string `validate:"permission_name"`
	WebhookURL string `validate:"request_url"`
}

type defaultValidatorInput struct {
	Age int `json:"age" validate:"gte=18"`
}

func TestStructValidator_Validate(t *testing.T) {
	sv, err := NewStructValidator()
	if err != nil {
		t.Fatalf("failed to create struct validator: %v", err)
	}

	t.Run("Valid User", func(t *testing.T) {
		u := TestUser{
			Password:   "P@ssword123!",
			Name:       "Alexis",
			Permission: "resource:action",
			WebhookURL: "https://example.com/webhook",
		}
		if apierr := sv.Validate(u); apierr != nil {
			t.Fatalf("expected valid struct, got error: %v", apierr)
		}
	})

	t.Run("Invalid Password", func(t *testing.T) {
		u := TestUser{
			Password:   "weak",
			Name:       "Alexis",
			Permission: "resource:action",
			WebhookURL: "https://example.com/webhook",
		}
		apierr := sv.Validate(u)
		if apierr == nil {
			t.Fatal("expected validation error, got nil")
		}
		if len(apierr.Details) != 1 || apierr.Details[0].Metadata["field"] != "Password" {
			t.Errorf("expected field error on Password, got details: %v", apierr.Details)
		}
	})

	t.Run("Blank Name", func(t *testing.T) {
		u := TestUser{
			Password:   "P@ssword123!",
			Name:       "   ",
			Permission: "resource:action",
			WebhookURL: "https://example.com/webhook",
		}
		apierr := sv.Validate(u)
		if apierr == nil {
			t.Fatal("expected validation error, got nil")
		}
		if len(apierr.Details) != 1 || apierr.Details[0].Metadata["field"] != "Name" {
			t.Errorf("expected field error on Name, got details: %v", apierr.Details)
		}
	})

	t.Run("Invalid Webhook Template URL", func(t *testing.T) {
		u := TestUser{
			Password:   "P@ssword123!",
			Name:       "Alexis",
			Permission: "resource:action",
			WebhookURL: "invalid_url_with_no_slash",
		}
		apierr := sv.Validate(u)
		if apierr == nil {
			t.Fatal("expected validation error for invalid URL")
		}
	})

	t.Run("Valid Webhook Template URL", func(t *testing.T) {
		u := TestUser{
			Password:   "P@ssword123!",
			Name:       "Alexis",
			Permission: "resource:action",
			WebhookURL: "https://{{domain}}/path",
		}
		if apierr := sv.Validate(u); apierr != nil {
			t.Fatalf("expected valid webhook template URL, got: %v", apierr)
		}
	})
}

func TestValidateStruct_DefaultValidator(t *testing.T) {
	apierr := ValidateStruct(defaultValidatorInput{Age: 12})
	if apierr == nil {
		t.Fatal("expected validation error, got nil")
	}
	if len(apierr.Details) != 1 {
		t.Fatalf("expected one validation detail, got %d", len(apierr.Details))
	}
	metadata := apierr.Details[0].Metadata
	if metadata["field"] != "age" {
		t.Fatalf("expected field metadata 'age', got %#v", metadata["field"])
	}
	value, ok := metadata["value"].(int)
	if !ok {
		t.Fatalf("expected raw int value metadata, got %#v", metadata["value"])
	}
	if value != 12 {
		t.Fatalf("expected raw value 12, got %d", value)
	}
}
