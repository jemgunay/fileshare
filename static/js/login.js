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
});