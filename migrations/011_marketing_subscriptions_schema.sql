DO $$
BEGIN
    IF to_regnamespace('commerce') IS NULL THEN
        CREATE SCHEMA commerce;
    END IF;

    IF to_regclass('commerce.marketing_subscriptions') IS NULL THEN
        IF to_regclass('public.marketing_subscriptions') IS NULL THEN
            RAISE EXCEPTION 'marketing_subscriptions does not exist in commerce or public';
        END IF;
        ALTER TABLE public.marketing_subscriptions SET SCHEMA commerce;
    END IF;
END $$;
