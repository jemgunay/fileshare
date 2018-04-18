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
                    $("#password-input").val("");
                    setButtonProcessing($("#login-btn"), false);
                }
                else if (result === "error") {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                    $("#password-input").val("");
                    setButtonProcessing($("#login-btn"), false);
                }
                else {
                    window.location = "/";
                }
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
                    setAlertWindow("success", "Please check your email.", "#error-window");
                    $("#email-input").val("");
                    setButtonProcessing($("#reset-btn"), false);
                }
                else {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                    $("#email-input").val("");
                    setButtonProcessing($("#reset-btn"), false);
                }
            });
        });
    }
});