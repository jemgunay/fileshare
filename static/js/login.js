$(document).ready(function() {
    // login page
    if (window.location.pathname === "/login") {
        setButtonProcessing($("#login-btn"), false);

        // login form submit
        $("#login-form").submit(function(e) {
            e.preventDefault();

            setButtonProcessing($("#login-btn"), true);
            var data = $(this).serialize();

            performRequest(hostname + "/login", "post", data, function(result) {
                result = result.trim();

                if (result === "unauthorised") {
                    setAlertWindow("warning", "Incorrect email address or password.", "#error-window");
                }
                else if (result === "error") {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                }
                else {
                    window.location = "/";
                }
                $("#password-input").val("");
                setButtonProcessing($("#login-btn"), false);
            });
        });
    }

    // reset page
    else if (window.location.pathname === "/reset") {
        setButtonProcessing($("#reset-btn"), false);

        // reset form submit
        $("#reset-form").submit(function (e) {
            e.preventDefault();

            setButtonProcessing($("#reset-btn"), true);
            var data = $(this).serialize();

            performRequest(hostname + "/reset/request", "post", data, function(result) {
                result = result.trim();

                if (result === "success") {
                    $("#reset-form").fadeOut(200);
                    setAlertWindow("success", "Your password reset request has been submitted!", "#error-window");
                }
                else {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                }

                $("#email-input").val("");
                setButtonProcessing($("#reset-btn"), false);
            });
        });
    }


    // create password page
    else if (window.location.pathname === "/") {
        setButtonProcessing($("#create-password-btn"), false);

        // reset form submit
        $("#create-password-form").submit(function (e) {
            e.preventDefault();

            setButtonProcessing($("#create-password-btn"), true);
            var data = $(this).serialize();

            performRequest(hostname + "/reset/set", "post", data, function(result) {
                result = JSON.parse(result.trim());

                if (result.status === "success") {
                    $("#reset-form").fadeOut(200);
                    window.location = "/";
                }
                else if (result.status === "warning") {
                    $("#password, #confirm-password").val("");
                    $("#password").focus();

                    if (result.value === "invalid_password_matching") {
                        setAlertWindow("warning", "Both passwords must match.", "#error-window");
                    }
                    else if (result.value === "invalid_password_empty") {
                        setAlertWindow("warning", "Password length must be a minimum of 8 characters.", "#error-window");
                    }
                    else if (result.value === "invalid_password_confirm_empty") {
                        setAlertWindow("warning", "Password length must be a minimum of 8 characters.", "#error-window");
                    }
                    else if (result.value === "invalid_password_length") {
                        setAlertWindow("warning", "Password length must be a minimum of 8 characters.", "#error-window");
                    }
                    else if (result.value === "invalid_password_lower") {
                        setAlertWindow("warning", "Password must contain at least one lowercase letter.", "#error-window");
                    }
                    else if (result.value === "invalid_password_upper") {
                        setAlertWindow("warning", "Password must contain at least one uppercase letter.", "#error-window");
                    }
                    else if (result.value === "invalid_password_number") {
                        setAlertWindow("warning", "Password must contain at least one number.", "#error-window");
                    }
                    else if (result.value === "invalid_password_special") {
                        setAlertWindow("warning", "Password must contain at least one special character.", "#error-window");
                    }

                    setButtonProcessing($("#create-password-btn"), false);
                }
                else {
                    logger.debugLog(result);
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                }
            });
        });
    }
});