package extmgr

import "github.com/sunfayao/persona/server/internal/mail"

func init() {
	NewMail = func(dir, secretKey string, store mailStore, google GoogleConfig) (Mail, error) {
		s, err := mail.NewWithKey(dir, secretKey, store)
		if err != nil {
			return nil, err
		}
		if err := s.SetGoogleProxy(google.Proxy); err != nil {
			s.Close()
			return nil, err
		}
		return s, nil
	}
}
