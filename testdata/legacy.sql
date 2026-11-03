BEGIN TRANSACTION;
CREATE TABLE changes (
    id       INTEGER PRIMARY KEY,
    ts       REAL NOT NULL,
    platform TEXT NOT NULL,
    slug     TEXT NOT NULL,
    kind     TEXT NOT NULL,          -- new / updated / removed
    data     TEXT NOT NULL           -- ProgramChange в JSON (без полных снапшотов)
);
INSERT INTO "changes" VALUES(1,123.0,'standoff','legacy','new','{"kind": "new", "name": "Legacy", "url": "https://scope.test/legacy", "activated": null, "assets_added": [], "rewards": []}');
CREATE TABLE health (
    platform   TEXT PRIMARY KEY,
    fails      INTEGER NOT NULL DEFAULT 0,
    alerted    INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
INSERT INTO "health" VALUES('standoff',2,1,'down');
CREATE TABLE outbox (
    id   INTEGER PRIMARY KEY,
    ts   REAL NOT NULL,
    html TEXT NOT NULL               -- готовое сообщение, ждёт отправки
);
INSERT INTO "outbox" VALUES(1,123.0,'pending from Python');
CREATE TABLE programs (
    platform TEXT NOT NULL,
    slug     TEXT NOT NULL,
    data     TEXT NOT NULL,          -- Program в JSON
    PRIMARY KEY (platform, slug)
);
INSERT INTO "programs" VALUES('standoff','legacy','{"platform": "standoff", "slug": "legacy", "name": "Legacy", "url": "https://scope.test/legacy", "active": true, "status": "", "rewards": {"critical": [1, 2]}, "assets": ["a.corp.ru"], "out_of_scope": [], "assets_source": "text", "scope_md": "", "versions": [], "updated_at": "", "meta": {"extractor": 2, "rewards_at": 123.0}}');
CREATE INDEX changes_program ON changes (platform, slug, ts);
COMMIT;
