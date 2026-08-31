package pg

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type PGError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *PGError) Error() string { return e.Message }

func normalizePGErr(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if as := errors.As(err, &pgErr); as {
		return &PGError{Code: pgErr.Code, Message: pgErr.Message, Detail: pgErr.Detail}
	}
	return err
}
