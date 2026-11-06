import pytest

import dashboard_server


@pytest.mark.parametrize("host", ["", " ", "0.0.0.0", "::", "10.0.0.5"])
def test_non_loopback_bind_requires_key(monkeypatch, host):
    monkeypatch.setattr(dashboard_server, "DASHBOARD_KEY", "")
    with pytest.raises(RuntimeError):
        dashboard_server._validate_bind(host)


@pytest.mark.parametrize("host", ["127.0.0.1", "localhost", "::1"])
def test_loopback_bind_allowed_without_key(monkeypatch, host):
    monkeypatch.setattr(dashboard_server, "DASHBOARD_KEY", "")
    dashboard_server._validate_bind(host)


@pytest.mark.parametrize("host", ["", "0.0.0.0"])
def test_wildcard_bind_allowed_with_key(monkeypatch, host):
    monkeypatch.setattr(dashboard_server, "DASHBOARD_KEY", "k")
    dashboard_server._validate_bind(host)
