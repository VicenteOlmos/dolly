CREATE TABLE IF NOT EXISTS departments (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    code TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS tbl_a (
    id SERIAL PRIMARY KEY,
    department_id INTEGER NOT NULL REFERENCES departments (id),
    name TEXT NOT NULL,
    nickname TEXT,
    hired_on DATE NOT NULL DEFAULT CURRENT_DATE,
    active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS projects (
    id SERIAL PRIMARY KEY,
    department_id INTEGER REFERENCES departments (id),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS project_members (
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    employee_id INTEGER NOT NULL REFERENCES tbl_a (id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'contributor',
    PRIMARY KEY (project_id, employee_id)
);

CREATE TABLE IF NOT EXISTS empty_audit (
    id SERIAL PRIMARY KEY,
    event TEXT
);

TRUNCATE project_members, projects, tbl_a, departments, empty_audit RESTART IDENTITY CASCADE;

INSERT INTO departments (id, name, code) VALUES
    (1, 'Engineering', 'ENG'),
    (2, 'Sales', 'SAL'),
    (3, 'People', 'HR'),
    (4, 'Operations', 'OPS');

INSERT INTO tbl_a (id, department_id, name, nickname, hired_on, active) VALUES
    (1, 1, 'Alice Chen', NULL, '2021-03-15', TRUE),
    (2, 1, 'Bob Martinez', 'Bobby', '2019-07-01', TRUE),
    (3, 1, 'Carol Okonkwo', NULL, '2023-01-10', TRUE),
    (4, 2, 'Dana Rivera', NULL, '2020-11-20', TRUE),
    (5, 2, 'Evan Brooks', 'Ev', '2018-05-08', FALSE),
    (6, 3, 'Frank Lee', NULL, '2022-09-01', TRUE),
    (7, 4, 'Grace Kim', NULL, '2017-12-12', TRUE),
    (8, 1, 'Henry Walsh', NULL, '2024-02-28', TRUE);

INSERT INTO projects (id, department_id, name, status) VALUES
    (1, 1, 'Dolly MVP', 'active'),
    (2, 1, 'Schema introspection', 'active'),
    (3, 2, 'Enterprise rollout', 'paused'),
    (4, NULL, 'Cross-team platform', 'active');

INSERT INTO project_members (project_id, employee_id, role) VALUES
    (1, 1, 'lead'),
    (1, 2, 'contributor'),
    (1, 3, 'contributor'),
    (1, 8, 'contributor'),
    (2, 1, 'lead'),
    (2, 3, 'contributor'),
    (3, 4, 'lead'),
    (3, 5, 'observer'),
    (4, 1, 'lead'),
    (4, 6, 'contributor'),
    (4, 7, 'contributor');
