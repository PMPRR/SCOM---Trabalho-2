--
-- PostgreSQL database dump
--

-- Dumped from database version 17.5 (Debian 17.5-1.pgdg110+1)
-- Dumped by pg_dump version 17.5 (Debian 17.5-1.pgdg110+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: tiger; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA tiger;


--
-- Name: tiger_data; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA tiger_data;


--
-- Name: topology; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA topology;


--
-- Name: SCHEMA topology; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON SCHEMA topology IS 'PostGIS Topology schema';


--
-- Name: fuzzystrmatch; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS fuzzystrmatch WITH SCHEMA public;


--
-- Name: EXTENSION fuzzystrmatch; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION fuzzystrmatch IS 'determine similarities and distance between strings';


--
-- Name: postgis; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS postgis WITH SCHEMA public;


--
-- Name: EXTENSION postgis; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION postgis IS 'PostGIS geometry and geography spatial types and functions';


--
-- Name: postgis_tiger_geocoder; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS postgis_tiger_geocoder WITH SCHEMA tiger;


--
-- Name: EXTENSION postgis_tiger_geocoder; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION postgis_tiger_geocoder IS 'PostGIS tiger geocoder and reverse geocoder';


--
-- Name: postgis_topology; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS postgis_topology WITH SCHEMA topology;


--
-- Name: EXTENSION postgis_topology; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION postgis_topology IS 'PostGIS topology spatial types and functions';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: institutes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.institutes (
    id smallint NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    website text,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT institutes_code_check CHECK ((code ~ '^[a-z0-9_]{2,20}$'::text)),
    CONSTRAINT institutes_name_check CHECK (((length(name) >= 1) AND (length(name) <= 120)))
);


--
-- Name: institutes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.institutes ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.institutes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: observations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.observations (
    station_id bigint NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    temperature_c real,
    humidity_pct real,
    precipitation_mm real,
    wind_speed_ms real,
    wind_dir_deg smallint,
    pressure_hpa real,
    ingested_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT observations_humidity_pct_check CHECK (((humidity_pct >= (0)::double precision) AND (humidity_pct <= (100)::double precision))),
    CONSTRAINT observations_precipitation_mm_check CHECK ((precipitation_mm >= (0)::double precision)),
    CONSTRAINT observations_pressure_hpa_check CHECK (((pressure_hpa >= (800)::double precision) AND (pressure_hpa <= (1100)::double precision))),
    CONSTRAINT observations_temperature_c_check CHECK (((temperature_c >= ('-90'::integer)::double precision) AND (temperature_c <= (60)::double precision))),
    CONSTRAINT observations_wind_dir_deg_check CHECK (((wind_dir_deg >= 0) AND (wind_dir_deg <= 360))),
    CONSTRAINT observations_wind_speed_ms_check CHECK ((wind_speed_ms >= (0)::double precision))
);


--
-- Name: stations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.stations (
    id bigint NOT NULL,
    institute_id smallint NOT NULL,
    external_code text NOT NULL,
    name text NOT NULL,
    location public.geography(Point,4326) NOT NULL,
    altitude_m real,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT stations_external_code_check CHECK (((length(external_code) >= 1) AND (length(external_code) <= 50))),
    CONSTRAINT stations_name_check CHECK (((length(name) >= 1) AND (length(name) <= 120)))
);


--
-- Name: stations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.stations ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.stations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    email text NOT NULL,
    name text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_email_check CHECK (((length(email) >= 3) AND (length(email) <= 254))),
    CONSTRAINT users_name_check CHECK (((length(name) >= 1) AND (length(name) <= 100))),
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['operator'::text, 'admin'::text])))
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.users ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: institutes institutes_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.institutes
    ADD CONSTRAINT institutes_code_key UNIQUE (code);


--
-- Name: institutes institutes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.institutes
    ADD CONSTRAINT institutes_pkey PRIMARY KEY (id);


--
-- Name: observations observations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.observations
    ADD CONSTRAINT observations_pkey PRIMARY KEY (station_id, observed_at);


--
-- Name: stations stations_institute_id_external_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stations
    ADD CONSTRAINT stations_institute_id_external_code_key UNIQUE (institute_id, external_code);


--
-- Name: stations stations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stations
    ADD CONSTRAINT stations_pkey PRIMARY KEY (id);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: observations_observed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX observations_observed_at_idx ON public.observations USING btree (observed_at);


--
-- Name: stations_location_gix; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX stations_location_gix ON public.stations USING gist (location);


--
-- Name: observations observations_station_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.observations
    ADD CONSTRAINT observations_station_id_fkey FOREIGN KEY (station_id) REFERENCES public.stations(id) ON DELETE CASCADE;


--
-- Name: stations stations_institute_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stations
    ADD CONSTRAINT stations_institute_id_fkey FOREIGN KEY (institute_id) REFERENCES public.institutes(id) ON DELETE RESTRICT;


--
-- PostgreSQL database dump complete
--

