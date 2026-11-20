import unittest

try:
    from db_env import postgres_env
except ImportError:
    def postgres_env(dsn):
        return {"PGDATABASE": dsn}


class DatabaseEnvironmentTests(unittest.TestCase):
    def test_uri_database_is_not_used_as_literal_database_name(self):
        env = postgres_env("postgres://qa:dummy@127.0.0.1:5432/testdb?sslmode=disable")
        self.assertEqual(env["PGDATABASE"], "testdb")
        self.assertEqual(env["PGUSER"], "qa")
        self.assertEqual(env["PGPASSWORD"], "dummy")

    def test_percent_encoded_password_is_preserved(self):
        env = postgres_env("postgres://qa:dummy%40test@localhost/testdb")
        self.assertEqual(env["PGPASSWORD"], "dummy@test")

    def test_keyword_connection_string_is_split_into_environment(self):
        env = postgres_env("host=127.0.0.1 port=5432 dbname=testdb user=qa password='dummy test'")
        self.assertEqual(env["PGDATABASE"], "testdb")
        self.assertEqual(env["PGPASSWORD"], "dummy test")

    def test_pgx_search_path_is_translated_for_libpq(self):
        env = postgres_env("postgres://qa:dummy@localhost/testdb?search_path=qa_schema")
        self.assertEqual(env["PGOPTIONS"], "-c search_path=qa_schema")
