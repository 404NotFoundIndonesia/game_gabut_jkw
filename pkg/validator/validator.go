package validator

import (
	stderrors "errors"
	"reflect"
	"strings"
	"sync"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	govalidator "github.com/go-playground/validator/v10"
)

var (
	instance *govalidator.Validate
	once     sync.Once
)

func get() *govalidator.Validate {
	once.Do(func() {
		instance = govalidator.New()
		// Use JSON tag names in field error messages.
		instance.RegisterTagNameFunc(func(fld reflect.StructField) string {
			tag := fld.Tag.Get("json")
			if idx := strings.Index(tag, ","); idx != -1 {
				tag = tag[:idx]
			}
			if tag == "" || tag == "-" {
				return fld.Name
			}
			return tag
		})
	})
	return instance
}

// Struct validates any struct using go-playground/validator struct tags.
// Returns a *apperrors.AppError with field-level details on failure, nil on success.
func Struct(v any) *apperrors.AppError {
	if err := get().Struct(v); err != nil {
		var validationErrors govalidator.ValidationErrors
		if stderrors.As(err, &validationErrors) {
			details := make([]apperrors.FieldError, 0, len(validationErrors))
			for _, fe := range validationErrors {
				details = append(details, apperrors.FieldError{
					Field:   fieldName(fe),
					Message: humanMessage(fe),
				})
			}
			return apperrors.Validation("request validation failed").WithDetails(details)
		}
		return apperrors.Validation(err.Error())
	}
	return nil
}

func fieldName(fe govalidator.FieldError) string {
	ns := fe.Namespace()
	// Namespace format: StructName.FieldName — drop struct prefix.
	if idx := strings.Index(ns, "."); idx != -1 {
		return ns[idx+1:]
	}
	return fe.Field()
}

func humanMessage(fe govalidator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		return "must be at least " + fe.Param()
	case "max":
		return "must be at most " + fe.Param()
	case "email":
		return "must be a valid email"
	case "uuid":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + fe.Param()
	default:
		return "failed validation: " + fe.Tag()
	}
}
