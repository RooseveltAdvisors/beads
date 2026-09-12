"""Integration tests for MCP tools."""

from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from beads_mcp.models import BlockedIssue, Comment, Issue, Stats, StatsSummary
from beads_mcp.tools import (
    beads_add_comment,
    beads_add_dependency,
    beads_add_note,
    beads_blocked,
    beads_claim_issue,
    beads_close_issue,
    beads_create_issue,
    beads_init,
    beads_list_comments,
    beads_list_issues,
    beads_quickstart,
    beads_ready_work,
    beads_reopen_issue,
    beads_show_issue,
    beads_stats,
    beads_update_issue,
)


@pytest.fixture(autouse=True)
def reset_connection_pool():
    """Reset connection pool before and after each test."""
    from beads_mcp import tools

    # Reset connection pool before each test
    tools._connection_pool.clear()
    yield
    # Reset connection pool after each test
    tools._connection_pool.clear()


@pytest.fixture
def sample_issue():
    """Create a sample issue for testing."""
    now = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    return Issue(
        id="bd-1",
        title="Test issue",
        description="Test description",
        status="open",
        priority=1,
        issue_type="bug",
        created_at=now,
        updated_at=now,
    )


@pytest.mark.asyncio
async def test_beads_ready_work(sample_issue):
    """Test beads_ready_work tool."""
    mock_client = AsyncMock()
    mock_client.ready = AsyncMock(return_value=[sample_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_ready_work(limit=10, priority=1)

    assert len(issues) == 1
    assert issues[0].id == "bd-1"
    mock_client.ready.assert_called_once()


@pytest.mark.asyncio
async def test_beads_ready_work_with_issue_type(sample_issue):
    """Test beads_ready_work passes issue_type to params."""
    mock_client = AsyncMock()
    mock_client.ready = AsyncMock(return_value=[sample_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_ready_work(limit=10, issue_type="bug")

    assert len(issues) == 1
    params = mock_client.ready.call_args[0][0]
    assert params.issue_type == "bug"


@pytest.mark.asyncio
async def test_beads_ready_work_no_params():
    """Test beads_ready_work with default parameters."""
    mock_client = AsyncMock()
    mock_client.ready = AsyncMock(return_value=[])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_ready_work()

    assert len(issues) == 0
    mock_client.ready.assert_called_once()


@pytest.mark.asyncio
async def test_beads_list_issues(sample_issue):
    """Test beads_list_issues tool."""
    mock_client = AsyncMock()
    mock_client.list_issues = AsyncMock(return_value=[sample_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_list_issues(status="open", priority=1)

    assert len(issues) == 1
    assert issues[0].id == "bd-1"
    mock_client.list_issues.assert_called_once()


@pytest.mark.asyncio
async def test_beads_show_issue(sample_issue):
    """Test beads_show_issue tool."""
    mock_client = AsyncMock()
    mock_client.show = AsyncMock(return_value=sample_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issue = await beads_show_issue(issue_id="bd-1")

    assert issue.id == "bd-1"
    assert issue.title == "Test issue"
    mock_client.show.assert_called_once()


@pytest.mark.asyncio
async def test_beads_create_issue(sample_issue):
    """Test beads_create_issue tool."""
    mock_client = AsyncMock()
    mock_client.get_config = AsyncMock(return_value="")
    mock_client.create = AsyncMock(return_value=sample_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issue = await beads_create_issue(
            title="New issue",
            description="New description",
            priority=2,
            issue_type="feature",
        )

    assert issue.id == "bd-1"
    mock_client.create.assert_called_once()


@pytest.mark.asyncio
async def test_beads_create_issue_with_labels(sample_issue):
    """Test beads_create_issue with labels."""
    mock_client = AsyncMock()
    mock_client.get_config = AsyncMock(return_value="")
    mock_client.create = AsyncMock(return_value=sample_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issue = await beads_create_issue(title="New issue", labels=["bug", "urgent"])

    assert issue.id == "bd-1"
    mock_client.create.assert_called_once()


@pytest.mark.asyncio
async def test_beads_create_issue_assigns_a_due_date_and_echoes_it_back():
    """Every created bead gets a deadline: an explicit due wins, otherwise the
    priority ladder supplies one and the result echoes the assigned date."""
    now = datetime(2024, 1, 1, tzinfo=timezone.utc)
    assigned = Issue(
        id="bd-9",
        title="New issue",
        status="open",
        priority=0,
        issue_type="task",
        created_at=now,
        updated_at=now,
        due_at=now,
    )
    mock_client = AsyncMock()
    mock_client.get_config = AsyncMock(return_value="")
    mock_client.create = AsyncMock(return_value=assigned)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        # Omitted due with due.required on: the priority ladder applies
        # (P0 = +1d, P4 = +30d) and the synthesis is stamped as a default.
        await beads_create_issue(title="New issue", priority=0)
        p0 = mock_client.create.call_args[0][0]
        assert p0.due == "+1d"
        assert p0.due_source == "default"

        await beads_create_issue(title="New issue", priority=4)
        p4 = mock_client.create.call_args[0][0]
        assert p4.due == "+30d"
        assert p4.due_source == "default"

        # An explicit due passes through untouched and stays caller-chosen.
        await beads_create_issue(title="New issue", priority=2, due="tomorrow")
        explicit = mock_client.create.call_args[0][0]
        assert explicit.due == "tomorrow"
        assert explicit.due_source is None


@pytest.mark.asyncio
async def test_beads_create_issue_ladder_follows_the_due_required_gate():
    """The MCP ladder fires only when the workspace requires due dates, and
    events stay undated exactly like `bd q` leaves them."""
    mock_client = AsyncMock()
    mock_client.create = AsyncMock(return_value=MagicMock())

    # A workspace that turned the invariant off gets no synthetic dates.
    mock_client.get_config = AsyncMock(return_value="false")
    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        await beads_create_issue(title="New issue", priority=2)
        relaxed = mock_client.create.call_args[0][0]
        assert relaxed.due is None
        assert relaxed.due_source is None

    # Events are exempt even while the invariant is on.
    mock_client.get_config = AsyncMock(return_value="true")
    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        await beads_create_issue(title="Audit", priority=2, issue_type="event")
        event = mock_client.create.call_args[0][0]
        assert event.due is None
        assert event.due_source is None


@pytest.mark.asyncio
async def test_beads_create_issue_result_carries_due_at(sample_issue):
    """The tool result carries the assigned deadline on due_at."""
    dated = sample_issue.model_copy(update={"due_at": datetime(2026, 3, 1, tzinfo=timezone.utc)})
    mock_client = AsyncMock()
    mock_client.get_config = AsyncMock(return_value="")
    mock_client.create = AsyncMock(return_value=dated)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issue = await beads_create_issue(title="New issue", priority=2)

    assert issue.due_at == datetime(2026, 3, 1, tzinfo=timezone.utc)


@pytest.mark.asyncio
async def test_beads_update_issue_due_and_repeat_reach_the_client(sample_issue):
    """The update tool forwards due (with force-gate fields) and repeat."""
    mock_client = AsyncMock()
    mock_client.update = AsyncMock(return_value=sample_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        await beads_update_issue(issue_id="bd-1", due="+3d", repeat="0 9 * * 1")
        gated = mock_client.update.call_args[0][0]
        assert gated.due == "+3d"
        assert gated.repeat == "0 9 * * 1"

        await beads_update_issue(
            issue_id="bd-1",
            due="",
            force_no_due=True,
            clear_due_reason="series ends here",
        )
        cleared = mock_client.update.call_args[0][0]
        assert cleared.due == ""
        assert cleared.force_no_due is True
        assert cleared.clear_due_reason == "series ends here"


@pytest.mark.asyncio
async def test_beads_update_issue(sample_issue):
    """Test beads_update_issue tool."""
    updated_issue = sample_issue.model_copy(update={"status": "blocked"})
    mock_client = AsyncMock()
    mock_client.update = AsyncMock(return_value=updated_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_update_issue(issue_id="bd-1", status="blocked")

    # Result can be Issue or list[Issue] depending on routing
    assert isinstance(result, Issue)
    assert result.status == "blocked"
    mock_client.update.assert_called_once()


@pytest.mark.asyncio
async def test_beads_claim_issue(sample_issue):
    """Test beads_claim_issue tool."""
    claimed_issue = sample_issue.model_copy(update={"status": "in_progress", "assignee": "agent-a"})
    mock_client = AsyncMock()
    mock_client.claim = AsyncMock(return_value=claimed_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_claim_issue(issue_id="bd-1")

    assert isinstance(result, Issue)
    assert result.status == "in_progress"
    assert result.assignee == "agent-a"
    mock_client.claim.assert_called_once()


@pytest.mark.asyncio
async def test_beads_close_issue(sample_issue):
    """Test beads_close_issue tool."""
    closed_issue = sample_issue.model_copy(update={"status": "closed", "closed_at": "2024-01-02T00:00:00Z"})
    mock_client = AsyncMock()
    mock_client.close = AsyncMock(return_value=[closed_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_close_issue(issue_id="bd-1", reason="Completed")

    assert len(issues) == 1
    assert issues[0].status == "closed"
    mock_client.close.assert_called_once()


@pytest.mark.asyncio
async def test_beads_reopen_issue(sample_issue):
    """Test beads_reopen_issue tool."""
    reopened_issue = sample_issue.model_copy(update={"status": "open", "closed_at": None})
    mock_client = AsyncMock()
    mock_client.reopen = AsyncMock(return_value=[reopened_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_reopen_issue(issue_ids=["bd-1"])

    assert len(issues) == 1
    assert issues[0].status == "open"
    assert issues[0].closed_at is None
    mock_client.reopen.assert_called_once()


@pytest.mark.asyncio
async def test_beads_reopen_multiple_issues(sample_issue):
    """Test beads_reopen_issue with multiple issues."""
    reopened_issue1 = sample_issue.model_copy(update={"id": "bd-1", "status": "open", "closed_at": None})
    reopened_issue2 = sample_issue.model_copy(update={"id": "bd-2", "status": "open", "closed_at": None})
    mock_client = AsyncMock()
    mock_client.reopen = AsyncMock(return_value=[reopened_issue1, reopened_issue2])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_reopen_issue(issue_ids=["bd-1", "bd-2"])

    assert len(issues) == 2
    assert issues[0].status == "open"
    assert issues[1].status == "open"
    assert all(issue.closed_at is None for issue in issues)
    mock_client.reopen.assert_called_once()


@pytest.mark.asyncio
async def test_beads_reopen_issue_with_reason(sample_issue):
    """Test beads_reopen_issue with reason parameter."""
    reopened_issue = sample_issue.model_copy(update={"status": "open", "closed_at": None})
    mock_client = AsyncMock()
    mock_client.reopen = AsyncMock(return_value=[reopened_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_reopen_issue(issue_ids=["bd-1"], reason="Found regression")

    assert len(issues) == 1
    assert issues[0].status == "open"
    assert issues[0].closed_at is None
    mock_client.reopen.assert_called_once()


@pytest.mark.asyncio
async def test_beads_add_dependency_success():
    """Test beads_add_dependency tool success."""
    mock_client = AsyncMock()
    mock_client.add_dependency = AsyncMock(return_value=None)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_add_dependency(issue_id="bd-2", depends_on_id="bd-1", dep_type="blocks")

    assert "Added dependency" in result
    assert "bd-2" in result
    assert "bd-1" in result
    mock_client.add_dependency.assert_called_once()


@pytest.mark.asyncio
async def test_beads_add_dependency_error():
    """Test beads_add_dependency tool error handling."""
    from beads_mcp.bd_client import BdError

    mock_client = AsyncMock()
    mock_client.add_dependency = AsyncMock(side_effect=BdError("Dependency already exists"))

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_add_dependency(issue_id="bd-2", depends_on_id="bd-1", dep_type="blocks")

    assert "Error" in result
    mock_client.add_dependency.assert_called_once()


@pytest.mark.asyncio
async def test_beads_add_comment_success():
    """Test beads_add_comment tool success."""
    mock_client = AsyncMock()
    mock_client.add_comment = AsyncMock(return_value="Added comment to bd-1")

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_add_comment(issue_id="bd-1", text="Fixed the race; tests pass")

    assert result == "Added comment to bd-1"
    mock_client.add_comment.assert_called_once()
    params = mock_client.add_comment.call_args[0][0]
    assert params.issue_id == "bd-1"
    assert params.text == "Fixed the race; tests pass"


@pytest.mark.asyncio
async def test_beads_add_comment_error_propagates():
    """Test that a BdError from the client propagates out of beads_add_comment."""
    from beads_mcp.bd_client import BdError

    mock_client = AsyncMock()
    mock_client.add_comment = AsyncMock(side_effect=BdError("Issue not found: bd-404"))

    with (
        patch("beads_mcp.tools._get_client", return_value=mock_client),
        pytest.raises(BdError, match="not found"),
    ):
        await beads_add_comment(issue_id="bd-404", text="orphan comment")

    mock_client.add_comment.assert_called_once()


@pytest.mark.asyncio
async def test_beads_list_comments_success():
    """Test beads_list_comments tool success."""
    comments = [
        Comment(
            id="c-1",
            issue_id="bd-1",
            author="agent",
            text="First comment",
            created_at=datetime(2026, 7, 7, 12, 0, 0, tzinfo=timezone.utc),
        ),
        Comment(
            id="c-2",
            issue_id="bd-1",
            author=None,
            text="Second comment",
            created_at=datetime(2026, 7, 7, 12, 5, 0, tzinfo=timezone.utc),
        ),
    ]
    mock_client = AsyncMock()
    mock_client.list_comments = AsyncMock(return_value=comments)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_list_comments(issue_id="bd-1")

    assert len(result) == 2
    assert result[0].text == "First comment"
    assert result[1].author is None
    mock_client.list_comments.assert_called_once()
    params = mock_client.list_comments.call_args[0][0]
    assert params.issue_id == "bd-1"


@pytest.mark.asyncio
async def test_beads_list_comments_error_propagates():
    """Test that a BdError from the client propagates out of beads_list_comments."""
    from beads_mcp.bd_client import BdError

    mock_client = AsyncMock()
    mock_client.list_comments = AsyncMock(side_effect=BdError("Issue not found: bd-404"))

    with (
        patch("beads_mcp.tools._get_client", return_value=mock_client),
        pytest.raises(BdError, match="not found"),
    ):
        await beads_list_comments(issue_id="bd-404")

    mock_client.list_comments.assert_called_once()


@pytest.mark.asyncio
async def test_beads_add_note_success():
    """Test beads_add_note tool success."""
    mock_client = AsyncMock()
    mock_client.add_note = AsyncMock(return_value="Appended note to bd-1")

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_add_note(issue_id="bd-1", text="Blocked on upstream PR")

    assert result == "Appended note to bd-1"
    mock_client.add_note.assert_called_once()
    params = mock_client.add_note.call_args[0][0]
    assert params.issue_id == "bd-1"
    assert params.text == "Blocked on upstream PR"


@pytest.mark.asyncio
async def test_beads_add_note_error_propagates():
    """Test that a BdError from the client propagates out of beads_add_note."""
    from beads_mcp.bd_client import BdError

    mock_client = AsyncMock()
    mock_client.add_note = AsyncMock(side_effect=BdError("Issue not found: bd-404"))

    with (
        patch("beads_mcp.tools._get_client", return_value=mock_client),
        pytest.raises(BdError, match="not found"),
    ):
        await beads_add_note(issue_id="bd-404", text="orphan note")

    mock_client.add_note.assert_called_once()


@pytest.mark.asyncio
async def test_beads_quickstart():
    """Test beads_quickstart tool."""
    quickstart_text = "# Beads Quickstart\n\nWelcome to beads..."
    mock_client = AsyncMock()
    mock_client.quickstart = AsyncMock(return_value=quickstart_text)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_quickstart()

    assert "Beads Quickstart" in result
    mock_client.quickstart.assert_called_once()


@pytest.mark.asyncio
async def test_client_lazy_initialization(tmp_path):
    """Test that client is lazily initialized on first use."""
    import os

    from beads_mcp import tools

    # Set workspace for the test
    test_workspace = str(tmp_path)
    os.environ["BEADS_WORKING_DIR"] = test_workspace

    # Clear connection pool before test
    tools._connection_pool.clear()

    # Mock create_bd_client to avoid actual bd calls
    mock_client_instance = AsyncMock()
    mock_client_instance.ready = AsyncMock(return_value=[])
    mock_client_instance.close = AsyncMock()

    try:
        with patch("beads_mcp.tools.create_bd_client") as mock_create_client:
            mock_create_client.return_value = mock_client_instance

            # First call should create client
            await beads_ready_work()

            # Verify create_bd_client was called
            assert mock_create_client.call_count >= 1

            # Verify client is now in pool
            assert len(tools._connection_pool) > 0

            # Second call should reuse client from pool
            call_count = mock_create_client.call_count
            await beads_ready_work()

            # Verify create_bd_client was not called again (or same count)
            assert mock_create_client.call_count == call_count
    finally:
        # Clean up environment
        if "BEADS_WORKING_DIR" in os.environ:
            del os.environ["BEADS_WORKING_DIR"]


@pytest.mark.asyncio
async def test_list_issues_with_all_filters(sample_issue):
    """Test beads_list_issues with all filter parameters."""
    mock_client = AsyncMock()
    mock_client.list_issues = AsyncMock(return_value=[sample_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        issues = await beads_list_issues(
            status="open",
            priority=1,
            issue_type="bug",
            assignee="user1",
            limit=100,
        )

    assert len(issues) == 1
    mock_client.list_issues.assert_called_once()


@pytest.mark.asyncio
async def test_update_issue_multiple_fields(sample_issue):
    """Test beads_update_issue with multiple fields."""
    updated_issue = sample_issue.model_copy(
        update={
            "status": "blocked",
            "priority": 0,
            "title": "Updated title",
        }
    )
    mock_client = AsyncMock()
    mock_client.update = AsyncMock(return_value=updated_issue)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_update_issue(
            issue_id="bd-1",
            status="blocked",
            priority=0,
            title="Updated title",
        )

    # Result can be Issue or list[Issue] depending on routing
    assert isinstance(result, Issue)
    assert result.status == "blocked"
    assert result.priority == 0
    assert result.title == "Updated title"
    mock_client.update.assert_called_once()


@pytest.mark.asyncio
async def test_update_issue_routes_closed_to_close(sample_issue):
    """Test that update with status=closed routes to close tool."""
    closed_issue = sample_issue.model_copy(update={"status": "closed", "closed_at": "2024-01-02T00:00:00Z"})
    mock_client = AsyncMock()
    mock_client.close = AsyncMock(return_value=[closed_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_update_issue(issue_id="bd-1", status="closed", notes="Task completed")

    # Should route to close, not update
    assert isinstance(result, list)
    assert len(result) == 1
    assert result[0].status == "closed"
    mock_client.close.assert_called_once()
    mock_client.update.assert_not_called()


@pytest.mark.asyncio
async def test_update_issue_routes_open_to_reopen(sample_issue):
    """Test that update with status=open routes to reopen tool."""
    reopened_issue = sample_issue.model_copy(update={"status": "open", "closed_at": None})
    mock_client = AsyncMock()
    mock_client.reopen = AsyncMock(return_value=[reopened_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_update_issue(issue_id="bd-1", status="open", notes="Needs more work")

    # Should route to reopen, not update
    assert isinstance(result, list)
    assert len(result) == 1
    assert result[0].status == "open"
    mock_client.reopen.assert_called_once()
    mock_client.update.assert_not_called()


@pytest.mark.asyncio
async def test_beads_stats():
    """Test beads_stats tool."""
    stats_data = Stats(
        summary=StatsSummary(
            total_issues=10,
            open_issues=5,
            in_progress_issues=2,
            closed_issues=3,
            blocked_issues=1,
            ready_issues=4,
            average_lead_time_hours=24.5,
        ),
        recent_activity=None,
    )
    mock_client = AsyncMock()
    mock_client.stats = AsyncMock(return_value=stats_data)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_stats()

    assert result.summary.total_issues == 10
    assert result.summary.open_issues == 5
    mock_client.stats.assert_called_once()


@pytest.mark.asyncio
async def test_beads_blocked():
    """Test beads_blocked tool."""
    now = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    blocked_issue = BlockedIssue(
        id="bd-1",
        title="Blocked issue",
        description="",
        status="blocked",
        priority=1,
        issue_type="bug",
        created_at=now,
        updated_at=now,
        blocked_by_count=2,
        blocked_by=["bd-2", "bd-3"],
    )
    mock_client = AsyncMock()
    mock_client.blocked = AsyncMock(return_value=[blocked_issue])

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_blocked()

    assert len(result) == 1
    assert result[0].id == "bd-1"
    assert result[0].blocked_by_count == 2
    mock_client.blocked.assert_called_once()


@pytest.mark.asyncio
async def test_beads_init():
    """Test beads_init tool."""
    init_output = "bd initialized successfully!"
    mock_client = AsyncMock()
    mock_client.init = AsyncMock(return_value=init_output)

    with patch("beads_mcp.tools._get_client", return_value=mock_client):
        result = await beads_init(prefix="test")

    assert "bd initialized successfully!" in result
    mock_client.init.assert_called_once()
