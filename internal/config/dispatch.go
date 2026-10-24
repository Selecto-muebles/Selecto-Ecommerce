package config

import (
	"errors"
)

func (c *Config) validateDispatch() error {
	if c.DispatchCutoffHour < 0 || c.DispatchCutoffHour > 23 {
		return errors.New("DISPATCH_CUTOFF_HOUR must be between 0 and 23")
	}
	phone := c.GestionarWhatsAppRecipient
	if phone == "" {
		return nil
	}
	if len(phone) < 8 || len(phone) > 15 {
		return errors.New("GESTIONAR_WHATSAPP_RECIPIENT must be 8-15 digits in international format, without +")
	}
	for _, digit := range phone {
		if digit < '0' || digit > '9' {
			return errors.New("GESTIONAR_WHATSAPP_RECIPIENT must be 8-15 digits in international format, without +")
		}
	}
	return nil
}
