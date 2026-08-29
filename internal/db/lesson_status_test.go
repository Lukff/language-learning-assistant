package db

import (
	"path/filepath"
	"testing"
)

func TestListLessonsWithStatus_ProcessingWhenNoJobsDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() unexpected error: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "processing" {
		t.Errorf("ListLessonsWithStatus() = %+v, expected status=processing", lessons)
	}
}

func TestListLessonsWithStatus_ReadyWhenTranscribeDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() unexpected error: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "ready" {
		t.Errorf("ListLessonsWithStatus() = %+v, expected status=ready", lessons)
	}
}

func TestListLessonsWithStatus_ErrorWithRootCauseMessage(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg not found", lessonID, "extract_audio"); err != nil {
		t.Fatalf("preparing fixture last_error failed: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depends on extract_audio which failed: ffmpeg not found", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparing fixture last_error failed: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() unexpected error: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "error" || lessons[0].ErrorMessage != "ffmpeg not found" {
		t.Errorf("ListLessonsWithStatus() = %+v, expected status=error with the extract_audio message (root cause, not the blocked transcribe one)", lessons)
	}
}

func TestListLessonsWithStatus_ErrorWhenOnlyTranscribeFailed(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "real STT failure", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparing fixture last_error failed: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() unexpected error: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "error" || lessons[0].ErrorMessage != "real STT failure" {
		t.Errorf("ListLessonsWithStatus() = %+v, expected status=error with the transcribe message", lessons)
	}
}

func TestListLessonsWithStatus_FiltersByTeacherAndPeriod(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	sarah := mustInsertLessonForJobs(t, conn, "sarah.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET lesson_date = ? WHERE id = ?`, "2026-07-10", sarah); err != nil {
		t.Fatalf("adjusting sarah fixture failed: %v", err)
	}
	mustInsertJob(t, conn, sarah, "extract_audio", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")
	mustInsertJob(t, conn, sarah, "transcribe", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")

	jamesTeacherID, err := GetOrCreateTeacherByName(conn, "James K.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() unexpected error: %v", err)
	}
	james := mustInsertLessonForJobs(t, conn, "james.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET teacher_id = ?, lesson_date = ? WHERE id = ?`, jamesTeacherID, "2026-07-20T14:00", james); err != nil {
		t.Fatalf("adjusting james fixture failed: %v", err)
	}
	mustInsertJob(t, conn, james, "extract_audio", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")
	mustInsertJob(t, conn, james, "transcribe", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")

	byTeacher, err := ListLessonsWithStatus(conn, LessonFilter{TeacherID: jamesTeacherID})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(TeacherID) unexpected error: %v", err)
	}
	if len(byTeacher) != 1 || byTeacher[0].TeacherName != "James K." {
		t.Errorf("ListLessonsWithStatus(TeacherID=james) = %+v, expected only James K.'s lesson", byTeacher)
	}

	byDate, err := ListLessonsWithStatus(conn, LessonFilter{DateFrom: "2026-07-15", DateTo: "2026-07-31"})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(DateFrom/DateTo) unexpected error: %v", err)
	}
	if len(byDate) != 1 || byDate[0].TeacherName != "James K." {
		t.Errorf("ListLessonsWithStatus(2026-07-15..2026-07-31) = %+v, expected only the 07/20 lesson (includes time, filters by date only)", byDate)
	}
}

func TestListLessonsWithStatus_FiltersByTopicsWithORSemantics(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	grammar, err := GetOrCreateTopicByName(conn, "grammar")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName(grammar) unexpected error: %v", err)
	}
	travel, err := GetOrCreateTopicByName(conn, "travel")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName(travel) unexpected error: %v", err)
	}
	work, err := GetOrCreateTopicByName(conn, "work")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName(work) unexpected error: %v", err)
	}

	grammarLesson := mustInsertLessonForJobs(t, conn, "grammar.mp4")
	if err := AddLessonTopic(conn, grammarLesson, grammar); err != nil {
		t.Fatalf("AddLessonTopic(grammar) unexpected error: %v", err)
	}

	travelLesson := mustInsertLessonForJobs(t, conn, "travel.mp4")
	if err := AddLessonTopic(conn, travelLesson, travel); err != nil {
		t.Fatalf("AddLessonTopic(travel) unexpected error: %v", err)
	}

	noTopicLesson := mustInsertLessonForJobs(t, conn, "no-topic.mp4")
	_ = noTopicLesson

	filtered, err := ListLessonsWithStatus(conn, LessonFilter{TopicIDs: []int64{grammar, travel}})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(TopicIDs) unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("ListLessonsWithStatus(TopicIDs=[grammar,travel]) = %+v, expected the 2 lessons with either topic", filtered)
	}

	byWork, err := ListLessonsWithStatus(conn, LessonFilter{TopicIDs: []int64{work}})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(TopicIDs=[work]) unexpected error: %v", err)
	}
	if len(byWork) != 0 {
		t.Errorf("ListLessonsWithStatus(TopicIDs=[work]) = %+v, expected empty (no lesson has the work topic)", byWork)
	}
}

func TestFindLessonWithStatusByID_FindsExistingWithStatusAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if err := SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() unexpected error: %v", err)
	}

	found, err := FindLessonWithStatusByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() unexpected error: %v", err)
	}
	if found == nil || found.Status != "ready" {
		t.Fatalf("FindLessonWithStatusByID() = %+v, expected status=ready", found)
	}
	if found.StudentSpeakerLabel == nil || *found.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("StudentSpeakerLabel = %v, expected speaker_0", found.StudentSpeakerLabel)
	}

	missing, err := FindLessonWithStatusByID(conn, lessonID+999)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() unexpected error: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonWithStatusByID() for a nonexistent id = %+v, expected nil", missing)
	}
}
