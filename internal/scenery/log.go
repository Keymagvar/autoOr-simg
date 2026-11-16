package scenery

import (
	"log/slog"

	"autoOr-simg/internal/logging"
)

func logFor() *slog.Logger {
	return logging.For("scenery")
}
