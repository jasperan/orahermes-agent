"""Global threading.Event for user interrupt signaling across all tools."""

import threading

_interrupt_event = threading.Event()


def set_interrupt(active: bool) -> None:
    """Called by the agent to signal or clear the interrupt."""
    if active:
        _interrupt_event.set()
    else:
        _interrupt_event.clear()


def is_interrupted() -> bool:
    """Check if an interrupt has been requested. Safe to call from any thread."""
    return _interrupt_event.is_set()
