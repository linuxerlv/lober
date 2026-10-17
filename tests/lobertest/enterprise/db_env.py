"""Convert pgx DSNs into libpq environment fields without password argv."""
import os
import shlex
import urllib.parse


def postgres_env(dsn):
    if "://" in dsn:
        url = urllib.parse.urlsplit(dsn)
        if url.scheme not in ("postgres", "postgresql"):
            raise ValueError("Unsupported database URI scheme")
        fields = dict(urllib.parse.parse_qsl(url.query))
        fields.update({"host": urllib.parse.unquote(url.hostname or ""),
                       "port": str(url.port or 5432),
                       "user": urllib.parse.unquote(url.username or ""),
                       "password": urllib.parse.unquote(url.password or ""),
                       "dbname": urllib.parse.unquote(url.path.lstrip("/"))})
    else:
        try:
            fields = dict(part.split("=", 1) for part in shlex.split(dsn))
        except ValueError:
            raise ValueError("Invalid database connection settings") from None
    mapping = {"host": "PGHOST", "hostaddr": "PGHOSTADDR", "port": "PGPORT",
               "user": "PGUSER", "password": "PGPASSWORD", "dbname": "PGDATABASE",
               "sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT",
               "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY", "options": "PGOPTIONS",
               "connect_timeout": "PGCONNECT_TIMEOUT", "application_name": "PGAPPNAME",
               "target_session_attrs": "PGTARGETSESSIONATTRS", "channel_binding": "PGCHANNELBINDING"}
    env = {key: value for key, value in os.environ.items() if not key.startswith("PG")}
    for key, value in fields.items():
        if key == "search_path":
            if not value or any(char.isspace() for char in value):
                raise ValueError("Unsupported whitespace in test schema setting")
            continue
        if key not in mapping:
            raise ValueError("Unsupported database connection option")
        if value:
            env[mapping[key]] = value
    if fields.get("search_path"):
        env["PGOPTIONS"] = (env.get("PGOPTIONS", "") + " -c search_path=" + fields["search_path"]).strip()
    env.setdefault("PGCONNECT_TIMEOUT", "10")
    return env
